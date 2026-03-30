package rain

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type modelAssignmentPlanKey struct {
	table     *schema.TableDef
	modelType reflect.Type
}

type modelAssignmentPlan struct {
	fields []plannedAssignmentField
}

type plannedAssignmentField struct {
	column *schema.ColumnDef
	index  []int
}

var modelAssignmentPlanCache sync.Map

func lookupModelAssignmentPlan(table *schema.TableDef, modelType reflect.Type) (*modelAssignmentPlan, error) {
	if table == nil {
		return nil, fmt.Errorf("rain: model assignment plan requires a non-nil table")
	}

	typ, err := structTypeForType(modelType)
	if err != nil {
		return nil, err
	}
	if _, err := lookupTableModelBinding(typ, table, true); err != nil {
		return nil, err
	}

	key := modelAssignmentPlanKey{table: table, modelType: typ}
	if cached, ok := modelAssignmentPlanCache.Load(key); ok {
		return cached.(*modelAssignmentPlan), nil
	}

	meta, err := lookupModelMetaForType(typ)
	if err != nil {
		return nil, err
	}

	plan := &modelAssignmentPlan{fields: make([]plannedAssignmentField, 0, len(table.Columns))}
	for _, column := range table.Columns {
		field, ok := meta.byColumn[column.Name]
		if !ok {
			continue
		}
		plan.fields = append(plan.fields, plannedAssignmentField{
			column: column,
			index:  append([]int(nil), field.index...),
		})
	}

	actual, _ := modelAssignmentPlanCache.LoadOrStore(key, plan)
	return actual.(*modelAssignmentPlan), nil
}
