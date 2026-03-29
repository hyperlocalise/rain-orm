package rain

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

type scannerInterface = interface {
	Scan(src any) error
}

type modelField struct {
	index []int
}

type modelMeta struct {
	byColumn   map[string]modelField
	byRelation map[string]modelField
}

type scanColumnPlan struct {
	discard    bool
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

	return lookupModelMetaForType(value.Type()), value, nil
}

func lookupModelMetaForType(typ reflect.Type) *modelMeta {
	if cached, ok := modelMetaCache.Load(typ); ok {
		return cached.(*modelMeta)
	}

	meta := &modelMeta{
		byColumn:   make(map[string]modelField, typ.NumField()),
		byRelation: make(map[string]modelField, typ.NumField()),
	}
	buildModelMeta(meta, typ, nil)
	actual, _ := modelMetaCache.LoadOrStore(typ, meta)

	return actual.(*modelMeta)
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

		columnName := field.Tag.Get("db")
		if columnName != "" && columnName != "-" {
			meta.byColumn[columnName] = modelField{index: current}
		}

		relationName := relationTagName(field.Tag.Get("rain"))
		if relationName != "" {
			meta.byRelation[relationName] = modelField{index: current}
		}
	}
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
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}

	target := value.Elem()
	switch target.Kind() {
	case reflect.Struct:
		plan, err := newRowScanPlan(rows, target.Type())
		if err != nil {
			return err
		}
		targets := make([]any, len(plan.columns))
		finalizers := make([]func() error, len(plan.columns))
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return sql.ErrNoRows
		}
		if err := scanCurrentRowWithPlan(rows, target, plan, targets, finalizers); err != nil {
			return err
		}
		return rows.Err()
	case reflect.Slice:
		elemType := target.Type().Elem()
		structType, pointerElems, err := sliceElementStructType(elemType)
		if err != nil {
			return err
		}
		plan, err := newRowScanPlan(rows, structType)
		if err != nil {
			return err
		}
		targets := make([]any, len(plan.columns))
		finalizers := make([]func() error, len(plan.columns))
		for rows.Next() {
			elemPtr := reflect.New(structType)
			if err := scanCurrentRowWithPlan(rows, elemPtr.Elem(), plan, targets, finalizers); err != nil {
				return err
			}
			if pointerElems {
				target.Set(reflect.Append(target, elemPtr))
				continue
			}
			target.Set(reflect.Append(target, elemPtr.Elem()))
		}
		return rows.Err()
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

func newRowScanPlan(rows *sql.Rows, modelType reflect.Type) (*rowScanPlan, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	meta := lookupModelMetaForType(modelType)
	plan := &rowScanPlan{columns: make([]scanColumnPlan, len(cols))}
	for idx, name := range cols {
		fieldInfo, ok := meta.byColumn[name]
		if !ok {
			plan.columns[idx] = scanColumnPlan{discard: true}
			continue
		}
		plan.columns[idx] = scanColumnPlan{fieldIndex: fieldInfo.index}
	}
	return plan, nil
}

func scanCurrentRowWithPlan(
	rows *sql.Rows,
	target reflect.Value,
	plan *rowScanPlan,
	targets []any,
	finalizers []func() error,
) error {
	for idx, column := range plan.columns {
		finalizers[idx] = nil
		if column.discard {
			var discard any
			targets[idx] = &discard
			continue
		}

		field, err := fieldByIndexAlloc(target, column.fieldIndex)
		if err != nil {
			return err
		}
		scanTarget, finalize, err := prepareScanTarget(field)
		if err != nil {
			return err
		}
		targets[idx] = scanTarget
		finalizers[idx] = finalize
	}

	if err := rows.Scan(targets...); err != nil {
		return err
	}

	for _, finalize := range finalizers {
		if finalize == nil {
			continue
		}
		if err := finalize(); err != nil {
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

func prepareScanTarget(field reflect.Value) (any, func() error, error) {
	if scanTarget, finalize, ok := scannerTarget(field); ok {
		return scanTarget, finalize, nil
	}

	if field.Kind() != reflect.Pointer {
		if !field.CanAddr() {
			return nil, nil, fmt.Errorf("rain: field %s is not addressable", field.Type())
		}
		return field.Addr().Interface(), nil, nil
	}

	for _, handler := range nullablePrimitiveHandlers() {
		if scanTarget, finalize, ok := handler(field); ok {
			return scanTarget, finalize, nil
		}
	}

	return nil, nil, fmt.Errorf("rain: unsupported nullable field type %s", field.Type())
}

type nullableHandler func(field reflect.Value) (target any, finalize func() error, ok bool)

func nullablePrimitiveHandlers() []nullableHandler {
	return []nullableHandler{
		nullableStringTarget,
		nullableSignedIntTarget,
		nullableUnsignedIntTarget,
		nullableFloatTarget,
		nullableBoolTarget,
		nullableTimeTarget,
	}
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

func nullableStringTarget(field reflect.Value) (any, func() error, bool) {
	if field.Type().Elem().Kind() != reflect.String {
		return nil, nil, false
	}

	holder := sql.Null[string]{}
	return &holder, func() error {
		if !holder.Valid {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		value := holder.V
		field.Set(reflect.ValueOf(&value))
		return nil
	}, true
}

func nullableSignedIntTarget(field reflect.Value) (any, func() error, bool) {
	elemType := field.Type().Elem()
	switch elemType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
	default:
		return nil, nil, false
	}

	holder := sql.Null[int64]{}
	return &holder, func() error {
		if !holder.Valid {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		ptr := reflect.New(elemType)
		ptr.Elem().SetInt(holder.V)
		field.Set(ptr)
		return nil
	}, true
}

func nullableUnsignedIntTarget(field reflect.Value) (any, func() error, bool) {
	elemType := field.Type().Elem()
	switch elemType.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return nil, nil, false
	}

	holder := sql.Null[int64]{}
	return &holder, func() error {
		if !holder.Valid {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		if holder.V < 0 {
			return fmt.Errorf("rain: cannot scan negative value %d into %s", holder.V, field.Type())
		}
		ptr := reflect.New(elemType)
		ptr.Elem().SetUint(uint64(holder.V))
		field.Set(ptr)
		return nil
	}, true
}

func nullableFloatTarget(field reflect.Value) (any, func() error, bool) {
	elemType := field.Type().Elem()
	if elemType.Kind() != reflect.Float32 && elemType.Kind() != reflect.Float64 {
		return nil, nil, false
	}

	holder := sql.Null[float64]{}
	return &holder, func() error {
		if !holder.Valid {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		ptr := reflect.New(elemType)
		ptr.Elem().SetFloat(holder.V)
		field.Set(ptr)
		return nil
	}, true
}

func nullableBoolTarget(field reflect.Value) (any, func() error, bool) {
	if field.Type().Elem().Kind() != reflect.Bool {
		return nil, nil, false
	}

	holder := sql.Null[bool]{}
	return &holder, func() error {
		if !holder.Valid {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		value := holder.V
		field.Set(reflect.ValueOf(&value))
		return nil
	}, true
}

func nullableTimeTarget(field reflect.Value) (any, func() error, bool) {
	if field.Type().Elem() != reflect.TypeFor[time.Time]() {
		return nil, nil, false
	}

	holder := sql.Null[time.Time]{}
	return &holder, func() error {
		if !holder.Valid {
			field.Set(reflect.Zero(field.Type()))
			return nil
		}
		value := holder.V
		field.Set(reflect.ValueOf(&value))
		return nil
	}, true
}
