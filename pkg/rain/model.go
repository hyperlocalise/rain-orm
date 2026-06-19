package rain

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
	"unsafe"

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
	byColumn         map[string]modelField
	byRelation       map[string]modelField
	allFieldsManaged bool
	numManagedFields int
	err              error
}

type scanColumnPlan struct {
	columnName   string
	scanIndex    int
	scratchIndex int
	fieldIndex   []int
	index0       int
	isComplex    bool
	isJSON       bool
	isDirect     bool
	columnDef    *schema.ColumnDef
	fieldType    reflect.Type

	// OPTIMIZATION: Bypassing reflection in the hot loop using unsafe offsets.
	offset       uintptr
	kind         reflect.Kind
	canUseOffset bool
}

type rowScanScratch struct {
	scanTargets []any
	scanned     []any

	ints    []sql.NullInt64
	strings []sql.NullString
	bools   []sql.NullBool
	floats  []sql.NullFloat64
	times   []sql.NullTime
}

type rowScanPlan struct {
	columns []scanColumnPlan

	// OPTIMIZATION: Track if we need a reflect.Value for any column.
	needsTargetValue bool

	// OPTIMIZATION: Track if this plan covers all fields of the target struct.
	isFullScan bool

	int64ValueCols   []scanColumnPlan
	int64PointerCols []scanColumnPlan

	stringValueCols   []scanColumnPlan
	stringPointerCols []scanColumnPlan

	boolValueCols   []scanColumnPlan
	boolPointerCols []scanColumnPlan

	float64ValueCols   []scanColumnPlan
	float64PointerCols []scanColumnPlan

	timeValueCols   []scanColumnPlan
	timePointerCols []scanColumnPlan

	otherCols []scanColumnPlan

	pool sync.Pool
}

type rowScanPlanKey struct {
	modelType reflect.Type

	// OPTIMIZATION: Avoid strings.Join for models with up to 10 columns by using
	// a fixed-size array. Larger column sets fall back to the dynamic string.
	columns      [10]string
	columnString string
	numColumns   int

	hasTable   bool
	tableName  string
	tableAlias string
}

var rowScanPlanCache sync.Map

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
		byColumn:         make(map[string]modelField, typ.NumField()),
		byRelation:       make(map[string]modelField, typ.NumField()),
		allFieldsManaged: true,
	}
	buildModelMeta(meta, typ, nil)
	actual, _ := modelMetaCache.LoadOrStore(typ, meta)

	resolved := actual.(*modelMeta)
	return resolved, resolved.err
}

func buildModelMeta(meta *modelMeta, typ reflect.Type, prefix []int) {
	for fieldIndex := range typ.NumField() {
		field := typ.Field(fieldIndex)

		current := append(append([]int{}, prefix...), fieldIndex)
		if embedded := embeddedStructType(field); embedded != nil {
			buildModelMeta(meta, embedded, current)
			continue
		}

		if field.PkgPath != "" {
			meta.allFieldsManaged = false
			continue
		}

		columnName, includeColumn, explicitColumn := columnNameForField(field)
		if includeColumn {
			addModelFieldMapping(meta.byColumn, &meta.err, columnName, current, "column", explicitColumn)
			meta.numManagedFields++
		} else {
			meta.allFieldsManaged = false
			relationName := relationTagName(field.Tag.Get("rain"))
			if relationName != "" {
				addModelFieldMapping(meta.byRelation, &meta.err, relationName, current, "relation", true)
			}
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
	if !target.CanSet() {
		return fmt.Errorf("rain: destination must be settable (pass a pointer to a slice or struct)")
	}

	if len(cols) == 1 {
		t := target.Type()
		isSlice := t.Kind() == reflect.Slice && !isBytesType(t)
		if isSlice {
			t = t.Elem()
		}
		if !isMappingStructType(t) {
			return scanSingleColumn(rows, target)
		}
	}

	switch target.Kind() {
	case reflect.Struct:
		plan, err := newRowScanPlanForColumns(cols, target.Type(), table)
		if err != nil {
			return err
		}

		scratch := plan.pool.Get().(*rowScanScratch)
		defer plan.pool.Put(scratch)

		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return sql.ErrNoRows
		}

		if err := rows.Scan(scratch.scanTargets...); err != nil {
			return err
		}

		var targetVal reflect.Value
		if plan.needsTargetValue {
			targetVal = target
		}
		return scanDirectRowAddr(target.Addr().UnsafePointer(), targetVal, plan, scratch)
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

		scratch := plan.pool.Get().(*rowScanScratch)
		defer plan.pool.Put(scratch)

		zeroElem := reflect.Zero(elemType)

		// Use a local slice header to grow the result set. If rows.Scan fails,
		// the original target slice remains unmodified (atomic-like behavior).
		// We use an addressable local to allow for SetLen optimizations.
		items := reflect.New(target.Type()).Elem()
		items.Set(target.Slice(0, 0))

		elemSize := elemType.Size()

		for rows.Next() {
			// OPTIMIZATION: Removed redundant clearing of scratch.scanned here.
			// Generic scanned values (for non-direct columns) are overwritten by
			// rows.Scan, and direct columns use separate scratch buffers.

			if err := rows.Scan(scratch.scanTargets...); err != nil {
				return err
			}

			// Grow the slice efficiently. Use SetLen if capacity is available to
			// avoid the heap allocations associated with reflect.Append.
			n := items.Len()
			if n < items.Cap() {
				items.SetLen(n + 1)
			} else {
				items.Set(reflect.Append(items, zeroElem))
			}

			// OPTIMIZATION: Derive element address directly from slice base pointer
			// to avoid Index(n) and Addr() overhead.
			ptr := unsafe.Add(unsafe.Pointer(items.Pointer()), uintptr(n)*elemSize)

			if pointerElems {
				newStruct := reflect.New(structType)
				reflect.NewAt(elemType, ptr).Elem().Set(newStruct)

				var targetVal reflect.Value
				if plan.needsTargetValue {
					targetVal = newStruct.Elem()
				}
				if err := scanDirectRowAddr(newStruct.UnsafePointer(), targetVal, plan, scratch); err != nil {
					return err
				}
			} else {
				var targetVal reflect.Value
				if plan.needsTargetValue {
					// Re-derive a reflect.Value for the element to reset it and handle non-offset columns.
					targetVal = reflect.NewAt(elemType, ptr).Elem()

					// OPTIMIZATION: Skip zeroing existing elements if the plan is a full scan.
					// This allows us to reuse existing pointer allocations in the struct fields.
					if !plan.isFullScan {
						// Reset existing element to its zero state before reuse to avoid data carry-over.
						targetVal.Set(zeroElem)
					}
				} else if !plan.isFullScan {
					// Even if we don't need a reflect.Value for assignments, we still need it to zero the struct
					// if this isn't a full scan to avoid data carry-over from previous rows.
					reflect.NewAt(elemType, ptr).Elem().Set(zeroElem)
				}

				if err := scanDirectRowAddr(ptr, targetVal, plan, scratch); err != nil {
					return err
				}
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

func newScanTargets(cols []string) ([]any, []any) {
	scanTargets := make([]any, len(cols))
	scanned := make([]any, len(cols))

	for idx := range cols {
		scanned[idx] = nil
		scanTargets[idx] = &scanned[idx]
	}

	return scanTargets, scanned
}

func scanDirectRow(target reflect.Value, plan *rowScanPlan, scratch *rowScanScratch) error {
	baseAddr := target.Addr().UnsafePointer()
	return scanDirectRowAddr(baseAddr, target, plan, scratch)
}

func scanDirectRowAddr(baseAddr unsafe.Pointer, target reflect.Value, plan *rowScanPlan, scratch *rowScanScratch) error {
	for i := range plan.int64ValueCols {
		col := &plan.int64ValueCols[i]
		v := &scratch.ints[col.scratchIndex]
		if !v.Valid {
			return fmt.Errorf("rain: cannot assign NULL to non-pointer field %s", col.fieldType)
		}

		if col.canUseOffset {
			ptr := unsafe.Add(baseAddr, col.offset)
			// OPTIMIZATION: Reordered cases to handle the most common database
			// types (Int64, Int, Int32) first to reduce branch mispredictions.
			switch col.kind {
			case reflect.Int64:
				*(*int64)(ptr) = v.Int64
			case reflect.Int:
				val := int(v.Int64)
				if int64(val) != v.Int64 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*int)(ptr) = val
			case reflect.Int32:
				val := int32(v.Int64)
				if int64(val) != v.Int64 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*int32)(ptr) = val
			case reflect.Int16:
				val := int16(v.Int64)
				if int64(val) != v.Int64 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*int16)(ptr) = val
			case reflect.Int8:
				val := int8(v.Int64)
				if int64(val) != v.Int64 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*int8)(ptr) = val
			case reflect.Uint64:
				if v.Int64 < 0 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*uint64)(ptr) = uint64(v.Int64)
			case reflect.Uint32:
				if v.Int64 < 0 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				val := uint32(v.Int64)
				if uint64(val) != uint64(v.Int64) {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*uint32)(ptr) = val
			case reflect.Uint16:
				if v.Int64 < 0 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				val := uint16(v.Int64)
				if uint64(val) != uint64(v.Int64) {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*uint16)(ptr) = val
			case reflect.Uint8:
				if v.Int64 < 0 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				val := uint8(v.Int64)
				if uint64(val) != uint64(v.Int64) {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*uint8)(ptr) = val
			case reflect.Uint:
				if v.Int64 < 0 {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				val := uint(v.Int64)
				if uint64(val) != uint64(v.Int64) {
					return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
				}
				*(*uint)(ptr) = val
			default:
				if !target.IsValid() {
					return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
				}
				field, err := fieldByIndexAlloc(target, col.fieldIndex)
				if err != nil {
					return err
				}
				if err := assignRawValueToField(field, v.Int64); err != nil {
					return err
				}
			}
			continue
		}

		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if field.Kind() == reflect.Int64 {
			field.SetInt(v.Int64)
		} else if field.Kind() >= reflect.Int && field.Kind() <= reflect.Int32 {
			if field.OverflowInt(v.Int64) {
				return fmt.Errorf("rain: value %d overflows field %s", v.Int64, field.Type())
			}
			field.SetInt(v.Int64)
		} else if field.Kind() >= reflect.Uint && field.Kind() <= reflect.Uint64 {
			if v.Int64 < 0 || field.OverflowUint(uint64(v.Int64)) {
				return fmt.Errorf("rain: value %d overflows field %s", v.Int64, field.Type())
			}
			field.SetUint(uint64(v.Int64))
		} else {
			if err := assignRawValueToField(field, v.Int64); err != nil {
				return err
			}
		}
	}
	for i := range plan.int64PointerCols {
		col := &plan.int64PointerCols[i]
		v := &scratch.ints[col.scratchIndex]

		if col.canUseOffset {
			ptr := (**int64)(unsafe.Add(baseAddr, col.offset))
			if !v.Valid {
				*ptr = nil
				continue
			}

			switch col.kind {
			case reflect.Pointer:
				elemKind := col.fieldType.Elem().Kind()
				// OPTIMIZATION: Prioritize Int64 and Int pointers for faster scanning.
				switch elemKind {
				case reflect.Int64:
					if *ptr == nil {
						*ptr = new(int64)
					}
					**ptr = v.Int64
				case reflect.Int:
					val := int(v.Int64)
					if int64(val) != v.Int64 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**int)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(int)
					}
					**p = val
				case reflect.Int32:
					val := int32(v.Int64)
					if int64(val) != v.Int64 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**int32)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(int32)
					}
					**p = val
				case reflect.Int16:
					val := int16(v.Int64)
					if int64(val) != v.Int64 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**int16)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(int16)
					}
					**p = val
				case reflect.Int8:
					val := int8(v.Int64)
					if int64(val) != v.Int64 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**int8)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(int8)
					}
					**p = val
				case reflect.Uint64:
					if v.Int64 < 0 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**uint64)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(uint64)
					}
					**p = uint64(v.Int64)
				case reflect.Uint32:
					if v.Int64 < 0 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					val := uint32(v.Int64)
					if uint64(val) != uint64(v.Int64) {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**uint32)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(uint32)
					}
					**p = val
				case reflect.Uint16:
					if v.Int64 < 0 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					val := uint16(v.Int64)
					if uint64(val) != uint64(v.Int64) {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**uint16)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(uint16)
					}
					**p = val
				case reflect.Uint8:
					if v.Int64 < 0 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					val := uint8(v.Int64)
					if uint64(val) != uint64(v.Int64) {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**uint8)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(uint8)
					}
					**p = val
				case reflect.Uint:
					if v.Int64 < 0 {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					val := uint(v.Int64)
					if uint64(val) != uint64(v.Int64) {
						return fmt.Errorf("rain: value %d overflows field %s", v.Int64, col.fieldType)
					}
					p := (**uint)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(uint)
					}
					**p = val
				default:
					if !target.IsValid() {
						return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
					}
					field, err := fieldByIndexAlloc(target, col.fieldIndex)
					if err != nil {
						return err
					}
					if err := assignRawValueToField(field, v.Int64); err != nil {
						return err
					}
				}
			}
			continue
		}

		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if !v.Valid {
			field.SetZero()
			continue
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
		if field.Kind() == reflect.Int64 {
			field.SetInt(v.Int64)
		} else if field.Kind() >= reflect.Int && field.Kind() <= reflect.Int32 {
			if field.OverflowInt(v.Int64) {
				return fmt.Errorf("rain: value %d overflows field %s", v.Int64, field.Type())
			}
			field.SetInt(v.Int64)
		} else if field.Kind() >= reflect.Uint && field.Kind() <= reflect.Uint64 {
			if v.Int64 < 0 || field.OverflowUint(uint64(v.Int64)) {
				return fmt.Errorf("rain: value %d overflows field %s", v.Int64, field.Type())
			}
			field.SetUint(uint64(v.Int64))
		} else {
			if err := assignRawValueToField(field, v.Int64); err != nil {
				return err
			}
		}
	}
	for i := range plan.stringValueCols {
		col := &plan.stringValueCols[i]
		v := &scratch.strings[col.scratchIndex]
		if !v.Valid {
			return fmt.Errorf("rain: cannot assign NULL to non-pointer field %s", col.fieldType)
		}
		if col.canUseOffset {
			*(*string)(unsafe.Add(baseAddr, col.offset)) = v.String
			continue
		}
		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if field.Kind() == reflect.String {
			field.SetString(v.String)
		} else {
			if err := assignRawValueToField(field, v.String); err != nil {
				return err
			}
		}
	}
	for i := range plan.stringPointerCols {
		col := &plan.stringPointerCols[i]
		v := &scratch.strings[col.scratchIndex]

		if col.canUseOffset {
			ptr := (**string)(unsafe.Add(baseAddr, col.offset))
			if !v.Valid {
				*ptr = nil
				continue
			}
			if *ptr == nil {
				*ptr = new(string)
			}
			**ptr = v.String
			continue
		}

		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if !v.Valid {
			field.SetZero()
			continue
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
		if field.Kind() == reflect.String {
			field.SetString(v.String)
		} else {
			if err := assignRawValueToField(field, v.String); err != nil {
				return err
			}
		}
	}
	for i := range plan.boolValueCols {
		col := &plan.boolValueCols[i]
		v := &scratch.bools[col.scratchIndex]
		if !v.Valid {
			return fmt.Errorf("rain: cannot assign NULL to non-pointer field %s", col.fieldType)
		}
		if col.canUseOffset {
			*(*bool)(unsafe.Add(baseAddr, col.offset)) = v.Bool
			continue
		}
		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if field.Kind() == reflect.Bool {
			field.SetBool(v.Bool)
		} else {
			if err := assignRawValueToField(field, v.Bool); err != nil {
				return err
			}
		}
	}
	for i := range plan.boolPointerCols {
		col := &plan.boolPointerCols[i]
		v := &scratch.bools[col.scratchIndex]

		if col.canUseOffset {
			ptr := (**bool)(unsafe.Add(baseAddr, col.offset))
			if !v.Valid {
				*ptr = nil
				continue
			}
			if *ptr == nil {
				*ptr = new(bool)
			}
			**ptr = v.Bool
			continue
		}

		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if !v.Valid {
			field.SetZero()
			continue
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
		if field.Kind() == reflect.Bool {
			field.SetBool(v.Bool)
		} else {
			if err := assignRawValueToField(field, v.Bool); err != nil {
				return err
			}
		}
	}
	for i := range plan.float64ValueCols {
		col := &plan.float64ValueCols[i]
		v := &scratch.floats[col.scratchIndex]
		if !v.Valid {
			return fmt.Errorf("rain: cannot assign NULL to non-pointer field %s", col.fieldType)
		}
		if col.canUseOffset {
			ptr := unsafe.Add(baseAddr, col.offset)
			switch col.kind {
			case reflect.Float64:
				*(*float64)(ptr) = v.Float64
			case reflect.Float32:
				f64 := v.Float64
				if f64 < 0 {
					f64 = -f64
				}
				if math.MaxFloat32 < f64 && f64 <= math.MaxFloat64 {
					return fmt.Errorf("rain: value %f overflows field %s", v.Float64, col.fieldType)
				}
				*(*float32)(ptr) = float32(v.Float64)
			}
			continue
		}
		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if field.Kind() == reflect.Float32 || field.Kind() == reflect.Float64 {
			if field.OverflowFloat(v.Float64) {
				return fmt.Errorf("rain: value %f overflows field %s", v.Float64, field.Type())
			}
			field.SetFloat(v.Float64)
		} else {
			if err := assignRawValueToField(field, v.Float64); err != nil {
				return err
			}
		}
	}
	for i := range plan.float64PointerCols {
		col := &plan.float64PointerCols[i]
		v := &scratch.floats[col.scratchIndex]

		if col.canUseOffset {
			ptr := (**float64)(unsafe.Add(baseAddr, col.offset))
			if !v.Valid {
				*ptr = nil
				continue
			}
			switch col.kind {
			case reflect.Pointer:
				elemKind := col.fieldType.Elem().Kind()
				switch elemKind {
				case reflect.Float64:
					if *ptr == nil {
						*ptr = new(float64)
					}
					**ptr = v.Float64
				case reflect.Float32:
					f64 := v.Float64
					if f64 < 0 {
						f64 = -f64
					}
					if math.MaxFloat32 < f64 && f64 <= math.MaxFloat64 {
						return fmt.Errorf("rain: value %f overflows field %s", v.Float64, col.fieldType)
					}
					p := (**float32)(unsafe.Pointer(ptr))
					if *p == nil {
						*p = new(float32)
					}
					**p = float32(v.Float64)
				}
			}
			continue
		}

		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if !v.Valid {
			field.SetZero()
			continue
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
		if field.Kind() == reflect.Float32 || field.Kind() == reflect.Float64 {
			if field.OverflowFloat(v.Float64) {
				return fmt.Errorf("rain: value %f overflows field %s", v.Float64, field.Type())
			}
			field.SetFloat(v.Float64)
		} else {
			if err := assignRawValueToField(field, v.Float64); err != nil {
				return err
			}
		}
	}
	for i := range plan.timeValueCols {
		col := &plan.timeValueCols[i]
		v := &scratch.times[col.scratchIndex]
		if !v.Valid {
			return fmt.Errorf("rain: cannot assign NULL to non-pointer field %s", col.fieldType)
		}
		if col.canUseOffset {
			*(*time.Time)(unsafe.Add(baseAddr, col.offset)) = v.Time
			continue
		}
		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if field.Type() == reflect.TypeFor[time.Time]() {
			*field.Addr().Interface().(*time.Time) = v.Time
		} else {
			if err := assignRawValueToField(field, v.Time); err != nil {
				return err
			}
		}
	}
	for i := range plan.timePointerCols {
		col := &plan.timePointerCols[i]
		v := &scratch.times[col.scratchIndex]

		if col.canUseOffset {
			ptr := (**time.Time)(unsafe.Add(baseAddr, col.offset))
			if !v.Valid {
				*ptr = nil
				continue
			}
			if *ptr == nil {
				*ptr = new(time.Time)
			}
			**ptr = v.Time
			continue
		}

		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for non-offset column %s", col.columnName)
		}
		field, err := fieldByIndexAlloc(target, col.fieldIndex)
		if err != nil {
			return err
		}
		if !v.Valid {
			field.SetZero()
			continue
		}
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
		if field.Type() == reflect.TypeFor[time.Time]() {
			*field.Addr().Interface().(*time.Time) = v.Time
		} else {
			if err := assignRawValueToField(field, v.Time); err != nil {
				return err
			}
		}
	}
	for i := range plan.otherCols {
		col := &plan.otherCols[i]
		if !target.IsValid() {
			return fmt.Errorf("rain: internal error: target is invalid for other column %s", col.columnName)
		}
		var field reflect.Value
		if col.isComplex {
			var err error
			field, err = fieldByIndexAlloc(target, col.fieldIndex)
			if err != nil {
				return err
			}
		} else {
			field = target.Field(col.index0)
		}
		rowVal := scratch.scanned[col.scanIndex]
		if !col.isDirect && col.isJSON {
			if s, ok := rowVal.(string); ok {
				rowVal = []byte(s)
			}
		}
		if err := assignRawValueToField(field, rowVal); err != nil {
			return err
		}
	}
	return nil
}

func scanCachedRowsAgainstTable(result *cachedSelectRows, dest any, table *schema.TableDef) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}

	target := value.Elem()

	if len(result.Columns) == 1 {
		t := target.Type()
		isSlice := t.Kind() == reflect.Slice && !isBytesType(t)
		if isSlice {
			t = t.Elem()
		}
		if !isMappingStructType(t) {
			return scanCachedSingleColumn(result, target, table)
		}
	}

	switch target.Kind() {
	case reflect.Struct:
		plan, err := newRowScanPlanForColumns(result.Columns, target.Type(), table)
		if err != nil {
			return err
		}
		if len(result.Rows) == 0 {
			return sql.ErrNoRows
		}
		if err := scanCachedRowWithPlan(result.Rows[0], target, plan); err != nil {
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

		items := reflect.MakeSlice(target.Type(), 0, len(result.Rows))
		for _, row := range result.Rows {
			var item reflect.Value
			var scanTarget reflect.Value
			if pointerElems {
				item = reflect.New(structType)
				scanTarget = item.Elem()
			} else {
				item = reflect.New(structType).Elem()
				scanTarget = item
			}

			if err := scanCachedRowWithPlan(row, scanTarget, plan); err != nil {
				return err
			}
			items = reflect.Append(items, item)
		}
		target.Set(items)
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
	key := rowScanPlanKey{
		modelType:  modelType,
		numColumns: len(cols),
	}
	// OPTIMIZATION: Populate the fixed-size column array for up to 10 columns
	// to avoid strings.Join allocations in the hot plan lookup path.
	if len(cols) <= 10 {
		copy(key.columns[:], cols)
	} else {
		key.columnString = strings.Join(cols, "\x00")
	}

	if table != nil {
		key.hasTable = true
		key.tableName = table.Name
		key.tableAlias = table.Alias
	}

	if cached, ok := rowScanPlanCache.Load(key); ok {
		return cached.(*rowScanPlan), nil
	}

	if err := validateScanColumnsAgainstTable(modelType, table, cols); err != nil {
		return nil, err
	}
	meta, err := lookupModelMetaForType(modelType)
	if err != nil {
		return nil, err
	}

	plan := &rowScanPlan{
		columns: make([]scanColumnPlan, 0, len(cols)),
	}

	var numInts, numStrings, numBools, numFloats, numTimes int

	matchedFields := make(map[string]struct{}, len(cols))
	for idx, name := range cols {
		fieldInfo, ok := meta.byColumn[name]
		if !ok {
			continue
		}
		matchedFields[name] = struct{}{}

		var columnDef *schema.ColumnDef
		isJSON := false
		if table != nil {
			columnDef, _ = table.ColumnByName(name)
			if columnDef != nil {
				isJSON = columnDef.Type.DataType == schema.TypeJSON || columnDef.Type.DataType == schema.TypeJSONB
			}
		}

		isComplex := len(fieldInfo.index) > 1
		index0 := -1
		if !isComplex {
			index0 = fieldInfo.index[0]
		}

		var fieldType reflect.Type
		var offset uintptr
		canUseOffset := true
		if isComplex {
			fieldType = modelType
			for _, i := range fieldInfo.index {
				if fieldType.Kind() == reflect.Pointer {
					fieldType = fieldType.Elem()
					canUseOffset = false
				}
				f := fieldType.Field(i)
				if canUseOffset {
					offset += f.Offset
				}
				fieldType = f.Type
			}
		} else {
			f := modelType.Field(index0)
			offset = f.Offset
			fieldType = f.Type
		}

		isDirect := !isJSON && isSimpleDirectType(fieldType)

		colPlan := scanColumnPlan{
			columnName:   name,
			scanIndex:    idx,
			fieldIndex:   fieldInfo.index,
			index0:       index0,
			isComplex:    isComplex,
			isJSON:       isJSON,
			isDirect:     isDirect,
			columnDef:    columnDef,
			fieldType:    fieldType,
			offset:       offset,
			kind:         fieldType.Kind(),
			canUseOffset: canUseOffset,
		}

		if !colPlan.canUseOffset {
			plan.needsTargetValue = true
		}

		if isDirect {
			isPtr := fieldType.Kind() == reflect.Pointer
			baseType := fieldType
			if isPtr {
				baseType = fieldType.Elem()
			}

			switch baseType.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				colPlan.scratchIndex = numInts
				numInts++
				if isPtr {
					plan.int64PointerCols = append(plan.int64PointerCols, colPlan)
				} else {
					plan.int64ValueCols = append(plan.int64ValueCols, colPlan)
				}
			case reflect.String:
				colPlan.scratchIndex = numStrings
				numStrings++
				if isPtr {
					plan.stringPointerCols = append(plan.stringPointerCols, colPlan)
				} else {
					plan.stringValueCols = append(plan.stringValueCols, colPlan)
				}
			case reflect.Bool:
				colPlan.scratchIndex = numBools
				numBools++
				if isPtr {
					plan.boolPointerCols = append(plan.boolPointerCols, colPlan)
				} else {
					plan.boolValueCols = append(plan.boolValueCols, colPlan)
				}
			case reflect.Float32, reflect.Float64:
				colPlan.scratchIndex = numFloats
				numFloats++
				if isPtr {
					plan.float64PointerCols = append(plan.float64PointerCols, colPlan)
				} else {
					plan.float64ValueCols = append(plan.float64ValueCols, colPlan)
				}
			case reflect.Struct:
				if baseType == reflect.TypeFor[time.Time]() {
					colPlan.scratchIndex = numTimes
					numTimes++
					if isPtr {
						plan.timePointerCols = append(plan.timePointerCols, colPlan)
					} else {
						plan.timeValueCols = append(plan.timeValueCols, colPlan)
					}
				} else {
					plan.needsTargetValue = true
					plan.otherCols = append(plan.otherCols, colPlan)
				}
			default:
				plan.needsTargetValue = true
				plan.otherCols = append(plan.otherCols, colPlan)
			}
		} else {
			plan.needsTargetValue = true
			plan.otherCols = append(plan.otherCols, colPlan)
		}
		plan.columns = append(plan.columns, colPlan)
	}

	if meta.allFieldsManaged && len(matchedFields) == meta.numManagedFields {
		plan.isFullScan = true
	}

	plan.pool.New = func() any {
		s := &rowScanScratch{
			scanTargets: make([]any, len(cols)),
			scanned:     make([]any, len(cols)),
			ints:        make([]sql.NullInt64, numInts),
			strings:     make([]sql.NullString, numStrings),
			bools:       make([]sql.NullBool, numBools),
			floats:      make([]sql.NullFloat64, numFloats),
			times:       make([]sql.NullTime, numTimes),
		}
		for i := range s.scanTargets {
			s.scanTargets[i] = &s.scanned[i]
		}
		for i := range plan.int64ValueCols {
			p := &plan.int64ValueCols[i]
			s.scanTargets[p.scanIndex] = &s.ints[p.scratchIndex]
		}
		for i := range plan.int64PointerCols {
			p := &plan.int64PointerCols[i]
			s.scanTargets[p.scanIndex] = &s.ints[p.scratchIndex]
		}
		for i := range plan.stringValueCols {
			p := &plan.stringValueCols[i]
			s.scanTargets[p.scanIndex] = &s.strings[p.scratchIndex]
		}
		for i := range plan.stringPointerCols {
			p := &plan.stringPointerCols[i]
			s.scanTargets[p.scanIndex] = &s.strings[p.scratchIndex]
		}
		for i := range plan.boolValueCols {
			p := &plan.boolValueCols[i]
			s.scanTargets[p.scanIndex] = &s.bools[p.scratchIndex]
		}
		for i := range plan.boolPointerCols {
			p := &plan.boolPointerCols[i]
			s.scanTargets[p.scanIndex] = &s.bools[p.scratchIndex]
		}
		for i := range plan.float64ValueCols {
			p := &plan.float64ValueCols[i]
			s.scanTargets[p.scanIndex] = &s.floats[p.scratchIndex]
		}
		for i := range plan.float64PointerCols {
			p := &plan.float64PointerCols[i]
			s.scanTargets[p.scanIndex] = &s.floats[p.scratchIndex]
		}
		for i := range plan.timeValueCols {
			p := &plan.timeValueCols[i]
			s.scanTargets[p.scanIndex] = &s.times[p.scratchIndex]
		}
		for i := range plan.timePointerCols {
			p := &plan.timePointerCols[i]
			s.scanTargets[p.scanIndex] = &s.times[p.scratchIndex]
		}
		return s
	}

	actual, _ := rowScanPlanCache.LoadOrStore(key, plan)
	return actual.(*rowScanPlan), nil
}

func isSimpleDirectType(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	scannerType := reflect.TypeFor[scannerInterface]()
	if t.Implements(scannerType) || reflect.PointerTo(t).Implements(scannerType) {
		return false
	}
	// OPTIMIZATION: Prioritize common database types to speed up metadata resolution.
	switch t.Kind() {
	case reflect.Int64, reflect.Int, reflect.Int32, reflect.String, reflect.Bool,
		reflect.Int16, reflect.Int8,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Struct:
		return t == reflect.TypeFor[time.Time]()
	}
	return false
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
	scanTargets, scanned := newScanTargets(cols)
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

func scanCachedRowWithPlan(row []cachedValue, target reflect.Value, plan *rowScanPlan) error {
	for _, column := range plan.columns {
		var field reflect.Value
		if column.isComplex {
			var err error
			field, err = fieldByIndexAlloc(target, column.fieldIndex)
			if err != nil {
				return err
			}
		} else {
			field = target.Field(column.index0)
		}
		if err := assignCachedValueToField(field, row[column.scanIndex], column.columnDef); err != nil {
			return err
		}
	}
	return nil
}

func fieldByIndexAlloc(value reflect.Value, index []int) (reflect.Value, error) {
	if len(index) == 1 {
		return value.Field(index[0]), nil
	}

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

func isMappingStructType(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	if t == reflect.TypeFor[time.Time]() {
		return false
	}
	return true
}

func scanSingleColumn(rows *sql.Rows, target reflect.Value) error {
	t := target.Type()
	isSlice := t.Kind() == reflect.Slice && !isBytesType(t)

	if isSlice {
		target.Set(target.Slice(0, 0))
		elemType := t.Elem()
		for rows.Next() {
			elem := reflect.New(elemType).Elem()
			if err := rows.Scan(elem.Addr().Interface()); err != nil {
				return err
			}
			target.Set(reflect.Append(target, elem))
		}
		return rows.Err()
	}

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}

	return rows.Scan(target.Addr().Interface())
}

func scanCachedSingleColumn(result *cachedSelectRows, target reflect.Value, table *schema.TableDef) error {
	if len(result.Rows) == 0 {
		t := target.Type()
		if t.Kind() == reflect.Slice && !isBytesType(t) {
			return nil
		}
		return sql.ErrNoRows
	}

	var columnDef *schema.ColumnDef
	if table != nil {
		columnDef, _ = table.ColumnByName(result.Columns[0])
	}

	t := target.Type()
	isSlice := t.Kind() == reflect.Slice && !isBytesType(t)

	if isSlice {
		elemType := t.Elem()
		items := reflect.MakeSlice(t, 0, len(result.Rows))
		for _, row := range result.Rows {
			elem := reflect.New(elemType).Elem()
			if err := assignCachedValueToField(elem, row[0], columnDef); err != nil {
				return err
			}
			items = reflect.Append(items, elem)
		}
		target.Set(items)
		return nil
	}

	return assignCachedValueToField(target, result.Rows[0][0], columnDef)
}
