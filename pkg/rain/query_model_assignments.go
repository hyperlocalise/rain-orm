package rain

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func assignmentsFromModel(table *schema.TableDef, model any, skipAuto bool) ([]assignment, error) {
	meta, value, err := lookupModelMeta(model)
	if err != nil {
		return nil, err
	}

	assignments := make([]assignment, 0, len(table.Columns))
	for _, column := range table.Columns {
		field, ok := meta.byColumn[column.Name]
		if !ok {
			continue
		}

		fieldValue := value.FieldByIndex(field.index)
		resolvedValue, include := fieldValueForInsert(column, fieldValue, skipAuto)
		if !include {
			continue
		}

		assignments = append(assignments, assignment{
			column: schema.Ref(column),
			value:  schema.ValueExpr{Value: resolvedValue},
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
	if column.Table.Name != table.Name {
		return fmt.Errorf("rain: column %s belongs to table %s, not %s", column.Name, column.Table.Name, table.Name)
	}
	if _, ok := table.ColumnByName(column.Name); !ok {
		return fmt.Errorf("rain: unknown column %s on table %s", column.Name, table.Name)
	}

	return nil
}

func fieldValueForInsert(column *schema.ColumnDef, fieldValue reflect.Value, skipAuto bool) (any, bool) {
	resolved, isNil := dereferenceValue(fieldValue)
	if isNil {
		return nil, false
	}

	if skipAuto && column.AutoIncrement && resolved.IsZero() {
		return nil, false
	}
	if column.HasDefault && resolved.IsZero() {
		return nil, false
	}

	return resolved.Interface(), true
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
