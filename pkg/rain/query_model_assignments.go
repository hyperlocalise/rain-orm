package rain

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func assignmentsFromModel(table *schema.TableDef, model any, skipAuto bool) ([]assignment, error) {
	_, value, err := lookupModelMeta(model)
	if err != nil {
		return nil, err
	}
	plan, err := lookupModelAssignmentPlan(table, value.Type())
	if err != nil {
		return nil, err
	}

	assignments := make([]assignment, 0, len(plan.fields))
	for _, field := range plan.fields {
		fieldValue := value.FieldByIndex(field.index)
		resolvedValue, include := fieldValueForInsert(field.column, fieldValue, skipAuto)
		if !include {
			continue
		}

		assignments = append(assignments, assignment{
			column: schema.Ref(field.column),
			value:  resolvedValue,
		})
	}

	return assignments, nil
}

func mergeAssignments(table *schema.TableDef, base, overrides []assignment) ([]assignment, error) {
	ordered := make([]assignment, 0, len(table.Columns))
	assignmentsByName := make(map[string]assignment, len(table.Columns))

	for _, item := range base {
		if err := validateAssignmentTarget(table, item); err != nil {
			return nil, err
		}
		assignmentsByName[item.column.ColumnDef().Name] = item
	}
	for _, item := range overrides {
		if err := validateAssignmentTarget(table, item); err != nil {
			return nil, err
		}
		assignmentsByName[item.column.ColumnDef().Name] = item
	}

	for _, column := range table.Columns {
		item, ok := assignmentsByName[column.Name]
		if !ok {
			continue
		}
		ordered = append(ordered, item)
		delete(assignmentsByName, column.Name)
	}

	if len(assignmentsByName) > 0 {
		names := make([]string, 0, len(assignmentsByName))
		for name := range assignmentsByName {
			names = append(names, name)
		}
		slices.Sort(names)
		return nil, fmt.Errorf("rain: insert assignments contain unknown target columns: %s", strings.Join(names, ", "))
	}

	return ordered, nil
}

func validateAssignmentTarget(table *schema.TableDef, item assignment) error {
	column := item.column.ColumnDef()
	if err := validateColumnBelongsToTable(table, column); err != nil {
		return err
	}
	if column.GeneratedExpr != nil {
		return fmt.Errorf("rain: cannot assign to generated column %s", column.Name)
	}

	return nil
}

func validateColumnBelongsToTable(table *schema.TableDef, column *schema.ColumnDef) error {
	if column.Table != table {
		return fmt.Errorf("rain: column %s belongs to table %s, not %s", column.Name, column.Table.Name, table.Name)
	}
	if _, ok := table.ColumnByName(column.Name); !ok {
		return fmt.Errorf("rain: unknown column %s on table %s", column.Name, table.Name)
	}
	return nil
}

func fieldValueForInsert(column *schema.ColumnDef, fieldValue reflect.Value, skipAuto bool) (any, bool) {
	if column.GeneratedExpr != nil {
		return nil, false
	}

	// OPTIMIZATION: Fast-path for common primitive types to check for zero values
	// before calling the more expensive insertValueForField.
	if skipAuto && column.AutoIncrement {
		switch fieldValue.Kind() {
		case reflect.Int64, reflect.Int, reflect.Int32, reflect.Int16, reflect.Int8:
			if val := fieldValue.Int(); val == 0 {
				return nil, false
			}
		case reflect.Uint64, reflect.Uint, reflect.Uint32, reflect.Uint16, reflect.Uint8:
			if val := fieldValue.Uint(); val == 0 {
				return nil, false
			}
		case reflect.String:
			if val := fieldValue.String(); val == "" {
				return nil, false
			}
		case reflect.Pointer:
			if fieldValue.IsNil() {
				return nil, false
			}
		}
	}

	resolvedValue, include, explicit := insertValueForField(fieldValue)
	if !include {
		return nil, false
	}

	if skipAuto && column.AutoIncrement && !explicit && isZeroInsertValue(resolvedValue) {
		return nil, false
	}

	return resolvedValue, true
}

func dereferenceValue(value reflect.Value) (reflect.Value, bool) {
	current := value
	for current.Kind() == reflect.Pointer {
		if current.IsNil() {
			return reflect.Value{}, true
		}
		current = current.Elem()
	}

	return current, false
}

func insertValueForField(fieldValue reflect.Value) (value any, include bool, explicit bool) {
	if !fieldValue.IsValid() {
		return nil, false, false
	}

	// OPTIMIZATION: Check for nil pointers before calling Interface() to avoid
	// redundant heap allocations from boxing nil pointers.
	kind := fieldValue.Kind()
	if kind == reflect.Pointer && fieldValue.IsNil() {
		return nil, false, false
	}

	// OPTIMIZATION: Fast-path for common primitive types to avoid interface
	// conversions and extra reflection overhead where possible.
	switch kind {
	case reflect.Int64:
		return fieldValue.Int(), true, false
	case reflect.String:
		return fieldValue.String(), true, false
	case reflect.Bool:
		return fieldValue.Bool(), true, false
	case reflect.Int:
		return fieldValue.Int(), true, false
	}

	if fieldValue.CanInterface() {
		if setter, ok := fieldValue.Interface().(setValueProvider); ok {
			value, include = setter.rainSetValue()
			return value, include, true
		}
	}

	if kind == reflect.Pointer {
		if fieldValue.Type().Implements(reflect.TypeFor[driver.Valuer]()) {
			return fieldValue.Interface(), true, true
		}
		value, include, _ = insertValueForField(fieldValue.Elem())
		return value, include, true
	}

	if fieldValue.CanAddr() && fieldValue.Addr().Type().Implements(reflect.TypeFor[driver.Valuer]()) {
		return fieldValue.Addr().Interface(), true, false
	}
	if fieldValue.Type().Implements(reflect.TypeFor[driver.Valuer]()) {
		return fieldValue.Interface(), true, false
	}

	return fieldValue.Interface(), true, false
}

func isZeroInsertValue(value any) bool {
	if value == nil {
		return true
	}

	// OPTIMIZATION: Use a type-switch fast-path for common database types to
	// avoid the heap allocations and overhead of reflect.ValueOf(value).IsZero().
	switch v := value.(type) {
	case int64:
		return v == 0
	case int:
		return v == 0
	case string:
		return v == ""
	case bool:
		return !v
	case time.Time:
		return v.IsZero()
	case float64:
		return v == 0
	case int32:
		return v == 0
	case int16:
		return v == 0
	case int8:
		return v == 0
	case uint:
		return v == 0
	case uint64:
		return v == 0
	case uint32:
		return v == 0
	case uint16:
		return v == 0
	case uint8:
		return v == 0
	case float32:
		return v == 0
	}

	return reflect.ValueOf(value).IsZero()
}
