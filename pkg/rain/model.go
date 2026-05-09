package rain

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type scannerInterface = interface {
	Scan(src any) error
}

type modelField struct {
	index          []int
	explicitColumn bool
}

type modelMeta struct {
	byColumn   map[string]modelField
	byRelation map[string]modelField
	err        error
}

type scanColumnPlan struct {
	discard    bool
	columnName string
	fieldIndex []int
}

type rowScanPlan struct {
	columns []scanColumnPlan
}

var modelMetaCache sync.Map

func lookupModelMeta(model any) (*modelMeta, reflect.Value, error) {
	value := reflect.ValueOf(model)
	if !value.IsValid() {
		return nil, reflect.Value{}, fmt.Errorf("rain: model cannot be nil")
	}

	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, reflect.Value{}, fmt.Errorf("rain: model pointer cannot be nil")
		}
		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return nil, reflect.Value{}, fmt.Errorf("rain: model must be a struct or pointer to struct")
	}

	meta, err := lookupModelMetaForType(value.Type())
	return meta, value, err
}

func lookupModelMetaForType(typ reflect.Type) (*modelMeta, error) {
	if cached, ok := modelMetaCache.Load(typ); ok {
		meta := cached.(*modelMeta)
		return meta, meta.err
	}

	meta := &modelMeta{
		byColumn:   make(map[string]modelField, typ.NumField()),
		byRelation: make(map[string]modelField, typ.NumField()),
	}
	buildModelMeta(meta, typ, nil)
	actual, _ := modelMetaCache.LoadOrStore(typ, meta)

	resolved := actual.(*modelMeta)
	return resolved, resolved.err
}

func buildModelMeta(meta *modelMeta, typ reflect.Type, prefix []int) {
	for fieldIndex := range typ.NumField() {
		field := typ.Field(fieldIndex)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}

		current := append(append([]int{}, prefix...), fieldIndex)
		if embedded := embeddedStructType(field); embedded != nil {
			buildModelMeta(meta, embedded, current)
			continue
		}

		columnName, includeColumn, explicitColumn := columnNameForField(field)
		if includeColumn {
			addModelFieldMapping(meta.byColumn, &meta.err, columnName, current, "column", explicitColumn)
		}

		relationName := relationTagName(field.Tag.Get("rain"))
		if relationName != "" {
			addModelFieldMapping(meta.byRelation, &meta.err, relationName, current, "relation", true)
		}
	}
}

func addModelFieldMapping(target map[string]modelField, errp *error, name string, index []int, kind string, explicit bool) {
	if _, ok := target[name]; ok {
		*errp = errors.Join(*errp, fmt.Errorf("rain: duplicate model field mapping for %s %q", kind, name))
		return
	}
	target[name] = modelField{index: index, explicitColumn: explicit}
}

func columnNameForField(field reflect.StructField) (string, bool, bool) {
	if raw, ok := field.Tag.Lookup("db"); ok {
		name := strings.TrimSpace(raw)
		if name == "" {
			return "", false, true
		}
		if name == "-" {
			return "", false, true
		}
		return name, true, true
	}
	if relationTagName(field.Tag.Get("rain")) != "" {
		return "", false, false
	}

	return snakeCaseIdentifier(field.Name), true, false
}

func embeddedStructType(field reflect.StructField) reflect.Type {
	if !field.Anonymous {
		return nil
	}

	typ := field.Type
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}

	return typ
}

func relationTagName(tag string) string {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" || trimmed == "-" {
		return ""
	}
	if relation, ok := strings.CutPrefix(trimmed, "relation:"); ok {
		return strings.TrimSpace(relation)
	}
	return ""
}

func scanRows(rows *sql.Rows, dest any) error {
	return scanRowsAgainstTable(rows, dest, nil)
}

func scanRowsAgainstTable(rows *sql.Rows, dest any, table *schema.TableDef) error {
	return scanRowsAgainstTableDirect(rows, dest, table)
}

func scanRowsAgainstTableDirect(rows *sql.Rows, dest any, table *schema.TableDef) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	target := value.Elem()
	switch target.Kind() {
	case reflect.Struct:
		plan, err := newRowScanPlanForColumns(cols, target.Type(), table)
		if err != nil {
			return err
		}
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return sql.ErrNoRows
		}

		scanTargets := make([]any, len(cols))
		scanned := make([]any, len(cols))
		for idx := range cols {
			scanTargets[idx] = &scanned[idx]
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return err
		}

		return scanDirectRowWithPlan(scanned, target, plan, table)
	case reflect.Slice:
		elemType := target.Type().Elem()
		structType, pointerElems, err := sliceElementStructType(elemType)
		if err != nil {
			return err
		}
		plan, err := newRowScanPlanForColumns(cols, structType, table)
		if err != nil {
			return err
		}

		scanTargets := make([]any, len(cols))
		scanned := make([]any, len(cols))
		for idx := range cols {
			scanTargets[idx] = &scanned[idx]
		}

		items := target
		for rows.Next() {
			for idx := range scanned {
				scanned[idx] = nil
			}
			if err := rows.Scan(scanTargets...); err != nil {
				return err
			}

			elemPtr := reflect.New(structType)
			if err := scanDirectRowWithPlan(scanned, elemPtr.Elem(), plan, table); err != nil {
				return err
			}
			if pointerElems {
				items = reflect.Append(items, elemPtr)
			} else {
				items = reflect.Append(items, elemPtr.Elem())
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		target.Set(items)
		return nil
	default:
		return fmt.Errorf("rain: destination must point to a struct or slice")
	}
}

func scanDirectRowWithPlan(scanned []any, target reflect.Value, plan *rowScanPlan, table *schema.TableDef) error {
	for idx, column := range plan.columns {
		if column.discard {
			continue
		}
		field, err := fieldByIndexAlloc(target, column.fieldIndex)
		if err != nil {
			return err
		}
		var columnDef *schema.ColumnDef
		if table != nil {
			columnDef, _ = table.ColumnByName(column.columnName)
		}
		if err := assignRawValueToFieldWithColumn(field, scanned[idx], columnDef); err != nil {
			return err
		}
	}
	return nil
}

func assignRawValueToFieldWithColumn(field reflect.Value, raw any, column *schema.ColumnDef) error {
	// If it's a JSON column and we got a string/bytes, it might need special handling in assignRawValueToField
	// but currently assignRawValueToField doesn't take column.
	// We can update assignRawValueToField or handle it here.
	if column != nil && (column.Type.DataType == schema.TypeJSON || column.Type.DataType == schema.TypeJSONB) {
		if s, ok := raw.(string); ok {
			raw = []byte(s)
		}
	}
	return assignRawValueToField(field, raw)
}

func scanCachedRowsAgainstTable(result *cachedSelectRows, dest any, table *schema.TableDef) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}

	target := value.Elem()
	switch target.Kind() {
	case reflect.Struct:
		plan, err := newRowScanPlanForColumns(result.Columns, target.Type(), table)
		if err != nil {
			return err
		}
		if len(result.Rows) == 0 {
			return sql.ErrNoRows
		}
		if err := scanCachedRowWithPlan(result.Rows[0], target, plan, table); err != nil {
			return err
		}
		return nil
	case reflect.Slice:
		elemType := target.Type().Elem()
		structType, pointerElems, err := sliceElementStructType(elemType)
		if err != nil {
			return err
		}
		plan, err := newRowScanPlanForColumns(result.Columns, structType, table)
		if err != nil {
			return err
		}
		for _, row := range result.Rows {
			elemPtr := reflect.New(structType)
			if err := scanCachedRowWithPlan(row, elemPtr.Elem(), plan, table); err != nil {
				return err
			}
			if pointerElems {
				target.Set(reflect.Append(target, elemPtr))
				continue
			}
			target.Set(reflect.Append(target, elemPtr.Elem()))
		}
		return nil
	default:
		return fmt.Errorf("rain: destination must point to a struct or slice")
	}
}

func sliceElementStructType(elemType reflect.Type) (reflect.Type, bool, error) {
	if elemType.Kind() == reflect.Struct {
		return elemType, false, nil
	}
	if elemType.Kind() == reflect.Pointer && elemType.Elem().Kind() == reflect.Struct {
		return elemType.Elem(), true, nil
	}
	return nil, false, fmt.Errorf("rain: destination slice element must be a struct or pointer to struct")
}

func newRowScanPlanForColumns(cols []string, modelType reflect.Type, table *schema.TableDef) (*rowScanPlan, error) {
	if err := validateScanColumnsAgainstTable(modelType, table, cols); err != nil {
		return nil, err
	}
	meta, err := lookupModelMetaForType(modelType)
	if err != nil {
		return nil, err
	}
	plan := &rowScanPlan{columns: make([]scanColumnPlan, len(cols))}
	for idx, name := range cols {
		fieldInfo, ok := meta.byColumn[name]
		if !ok {
			plan.columns[idx] = scanColumnPlan{discard: true, columnName: name}
			continue
		}
		plan.columns[idx] = scanColumnPlan{columnName: name, fieldIndex: fieldInfo.index}
	}
	return plan, nil
}

func readCachedSelectRows(rows *sql.Rows) (*cachedSelectRows, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := &cachedSelectRows{
		Columns: append([]string(nil), cols...),
		Rows:    make([][]cachedValue, 0),
	}
	scanTargets := make([]any, len(cols))
	scanned := make([]any, len(cols))
	for idx := range cols {
		scanTargets[idx] = &scanned[idx]
	}
	for rows.Next() {
		for idx := range scanned {
			scanned[idx] = nil
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return nil, err
		}
		row := make([]cachedValue, len(scanned))
		for idx, value := range scanned {
			cell, err := encodeCachedValue(value)
			if err != nil {
				return nil, err
			}
			row[idx] = cell
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func scanCachedRowWithPlan(row []cachedValue, target reflect.Value, plan *rowScanPlan, table *schema.TableDef) error {
	for idx, column := range plan.columns {
		if column.discard {
			continue
		}
		field, err := fieldByIndexAlloc(target, column.fieldIndex)
		if err != nil {
			return err
		}
		var columnDef *schema.ColumnDef
		if table != nil {
			columnDef, _ = table.ColumnByName(column.columnName)
		}
		if err := assignCachedValueToField(field, row[idx], columnDef); err != nil {
			return err
		}
	}
	return nil
}

func fieldByIndexAlloc(value reflect.Value, index []int) (reflect.Value, error) {
	current := value
	for position, part := range index {
		field := current.Field(part)
		if position < len(index)-1 && field.Kind() == reflect.Pointer {
			if field.IsNil() {
				if !field.CanSet() {
					return reflect.Value{}, fmt.Errorf("rain: embedded pointer field %s is not settable", field.Type())
				}
				field.Set(reflect.New(field.Type().Elem()))
			}
			current = field.Elem()
			continue
		}
		current = field
	}
	return current, nil
}

func scannerTarget(field reflect.Value) (any, func() error, bool) {
	scannerType := reflect.TypeFor[scannerInterface]()

	if field.Kind() != reflect.Pointer {
		if field.CanAddr() && field.Addr().Type().Implements(scannerType) {
			return field.Addr().Interface(), nil, true
		}
		return nil, nil, false
	}

	fieldType := field.Type()
	if fieldType.Implements(scannerType) {
		receiver := reflect.New(fieldType.Elem())
		return receiver.Interface(), func() error {
			field.Set(receiver)
			return nil
		}, true
	}

	if fieldType.Elem().Implements(scannerType) {
		receiver := reflect.New(fieldType.Elem())
		return receiver.Interface(), func() error {
			field.Set(receiver)
			return nil
		}, true
	}

	return nil, nil, false
}

func assignCachedValueToField(field reflect.Value, value cachedValue, column *schema.ColumnDef) error {
	raw, err := decodeCachedValue(value, column)
	if err != nil {
		return err
	}
	return assignRawValueToField(field, raw)
}

func assignRawValueToField(field reflect.Value, raw any) error {
	if scanTarget, finalize, ok := scannerTarget(field); ok {
		scanner := scanTarget.(scannerInterface)
		if err := scanner.Scan(raw); err != nil {
			return err
		}
		if finalize != nil {
			return finalize()
		}
		return nil
	}

	if raw == nil {
		if field.Kind() == reflect.Pointer {
			field.SetZero()
			return nil
		}
		return fmt.Errorf("rain: cannot assign NULL to non-pointer field %s", field.Type())
	}

	if field.Kind() == reflect.Pointer {
		if !supportsCachedPointerAssignment(field.Type()) {
			return fmt.Errorf("rain: unsupported nullable field type %s", field.Type())
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return assignRawValueToField(field.Elem(), raw)
	}

	switch field.Kind() {
	case reflect.String:
		switch value := raw.(type) {
		case string:
			field.SetString(value)
			return nil
		case []byte:
			field.SetString(string(value))
			return nil
		case time.Time:
			field.SetString(value.Format(time.RFC3339Nano))
			return nil
		}
	case reflect.Bool:
		switch value := raw.(type) {
		case bool:
			field.SetBool(value)
			return nil
		case int64:
			field.SetBool(value != 0)
			return nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value, ok := raw.(int64); ok {
			if field.OverflowInt(value) {
				return fmt.Errorf("rain: value %d overflows field %s", value, field.Type())
			}
			field.SetInt(value)
			return nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if value, ok := raw.(int64); ok {
			if value < 0 || field.OverflowUint(uint64(value)) {
				return fmt.Errorf("rain: value %d overflows field %s", value, field.Type())
			}
			field.SetUint(uint64(value))
			return nil
		}
	case reflect.Float32, reflect.Float64:
		if value, ok := raw.(float64); ok {
			if field.OverflowFloat(value) {
				return fmt.Errorf("rain: value %f overflows field %s", value, field.Type())
			}
			field.SetFloat(value)
			return nil
		}
	case reflect.Slice:
		if isBytesType(field.Type()) {
			if value, ok := raw.([]byte); ok {
				field.SetBytes(value)
				return nil
			}
			if value, ok := raw.(string); ok {
				field.SetBytes([]byte(value))
				return nil
			}
		}
	case reflect.Struct:
		if field.Type() == reflect.TypeFor[time.Time]() {
			if value, ok := raw.(time.Time); ok {
				field.Set(reflect.ValueOf(value))
				return nil
			}
		}
	}

	if converted := reflect.ValueOf(raw); converted.IsValid() && converted.Type().AssignableTo(field.Type()) {
		field.Set(converted)
		return nil
	}

	return fmt.Errorf("rain: cannot assign cached %T to field %s", raw, field.Type())
}

func supportsCachedPointerAssignment(typ reflect.Type) bool {
	if typ.Kind() != reflect.Pointer {
		return true
	}
	elem := typ.Elem()
	switch elem.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.Bool:
		return true
	case reflect.Slice:
		return isBytesType(elem)
	case reflect.Struct:
		return elem == reflect.TypeFor[time.Time]() || supportsScanner(typ) || supportsScanner(elem)
	default:
		return supportsScanner(typ) || supportsScanner(elem)
	}
}

func snakeCaseIdentifier(name string) string {
	if name == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(name) + len(name)/2)
	runes := []rune(name)
	for idx, current := range runes {
		if idx > 0 && shouldInsertUnderscore(runes, idx) {
			builder.WriteByte('_')
		}
		builder.WriteRune(toLowerRune(current))
	}

	return builder.String()
}

func shouldInsertUnderscore(runes []rune, idx int) bool {
	current := runes[idx]
	prev := runes[idx-1]

	if !isUpperASCII(current) {
		return false
	}
	if isLowerASCII(prev) || isDigitASCII(prev) {
		return true
	}
	if idx+1 < len(runes) && isLowerASCII(runes[idx+1]) {
		return true
	}

	return false
}

func isUpperASCII(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func isLowerASCII(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func isDigitASCII(r rune) bool {
	return r >= '0' && r <= '9'
}

func toLowerRune(r rune) rune {
	if isUpperASCII(r) {
		return r + ('a' - 'A')
	}
	return r
}
