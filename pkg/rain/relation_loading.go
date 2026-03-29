package rain

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type typedKey struct {
	typeName string
	value    string
}

func (q *SelectQuery) scanRowsWithRelations(ctx context.Context, rows *sql.Rows, dest any) error {
	value := reflect.ValueOf(dest)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("rain: destination must be a non-nil pointer")
	}

	target := value.Elem()
	isSingle := target.Kind() == reflect.Struct
	if target.Kind() != reflect.Struct && target.Kind() != reflect.Slice {
		return fmt.Errorf("rain: destination must point to a struct or slice")
	}
	if target.Kind() == reflect.Slice && target.Type().Elem().Kind() != reflect.Struct {
		return fmt.Errorf("rain: relation loading requires destination slice element type to be a struct")
	}

	if _, ok := q.table.(tableDefSource); !ok {
		return fmt.Errorf("rain: relation loading requires a concrete table source")
	}

	containerPtr := dest
	if isSingle {
		sliceType := reflect.SliceOf(target.Type())
		slicePtr := reflect.New(sliceType)
		containerPtr = slicePtr.Interface()
	}

	if err := scanRows(rows, containerPtr); err != nil {
		return err
	}

	sliceValue := reflect.ValueOf(containerPtr).Elem()
	if err := q.loadRelationsIntoSlice(ctx, sliceValue); err != nil {
		return err
	}

	if isSingle {
		if sliceValue.Len() == 0 {
			return sql.ErrNoRows
		}
		target.Set(sliceValue.Index(0))
	}

	return nil
}

func (q *SelectQuery) loadRelationsIntoSlice(ctx context.Context, parents reflect.Value) error {
	tableSource, ok := q.table.(tableDefSource)
	if !ok {
		return fmt.Errorf("rain: relation loading requires a concrete table source")
	}

	if parents.Len() == 0 {
		return nil
	}

	for _, relationName := range q.relationNames {
		relation, exists := tableSource.table.RelationByName(relationName)
		if !exists {
			return fmt.Errorf("rain: unknown relation %q on table %q", relationName, tableSource.table.Name)
		}
		if err := q.loadRelation(ctx, parents, relation); err != nil {
			return err
		}
	}

	return nil
}

func (q *SelectQuery) loadRelation(ctx context.Context, parents reflect.Value, relation schema.RelationDef) error {
	for idx := 0; idx < parents.Len(); idx++ {
		parent := parents.Index(idx)
		if err := q.validateRelationField(parent, relation); err != nil {
			return err
		}
	}

	sourceKeys := make(map[typedKey]any, parents.Len())
	for idx := 0; idx < parents.Len(); idx++ {
		parent := parents.Index(idx)
		keyValue, ok, err := relationColumnValue(parent, relation.SourceColumn.Name)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		sourceKeys[toTypedKey(keyValue)] = keyValue
	}
	if len(sourceKeys) == 0 {
		return nil
	}

	relatedByTargetKey := make(map[typedKey][]reflect.Value, len(sourceKeys))
	relatedElemType, err := q.relationElementType(parents.Index(0), relation)
	if err != nil {
		return err
	}
	for _, sourceKey := range sourceKeys {
		query := &SelectQuery{runner: q.runner, dialect: q.dialect, table: tableDefSource{table: relation.TargetTable}}
		relatedRows := reflect.New(reflect.SliceOf(relatedElemType))
		if err := query.Where(schema.ComparisonExpr{Left: schema.Ref(relation.TargetColumn), Operator: "=", Right: schema.ValueExpr{Value: sourceKey}}).
			Scan(ctx, relatedRows.Interface()); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return err
		}
		for rowIdx := 0; rowIdx < relatedRows.Elem().Len(); rowIdx++ {
			related := relatedRows.Elem().Index(rowIdx)
			targetValue, ok, err := relationColumnValue(related, relation.TargetColumn.Name)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			relatedByTargetKey[toTypedKey(targetValue)] = append(relatedByTargetKey[toTypedKey(targetValue)], related)
		}
	}

	for idx := 0; idx < parents.Len(); idx++ {
		parent := parents.Index(idx)
		sourceValue, ok, err := relationColumnValue(parent, relation.SourceColumn.Name)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		matches := relatedByTargetKey[toTypedKey(sourceValue)]
		if err := setRelationValue(parent, relation.Name, relation.Type, matches); err != nil {
			return err
		}
	}

	return nil
}

func (q *SelectQuery) validateRelationField(parent reflect.Value, relation schema.RelationDef) error {
	meta, _, err := lookupModelMeta(parent.Addr().Interface())
	if err != nil {
		return err
	}
	fieldInfo, ok := meta.byRelation[relation.Name]
	if !ok {
		return fmt.Errorf("rain: relation %q requires a struct field tagged with `rain:\"relation:%s\"`", relation.Name, relation.Name)
	}
	field := parent.FieldByIndex(fieldInfo.index)
	switch relation.Type {
	case schema.RelationTypeBelongsTo:
		if field.Kind() != reflect.Struct && field.Kind() != reflect.Pointer {
			return fmt.Errorf("rain: relation %q must target a struct or pointer-to-struct field", relation.Name)
		}
	case schema.RelationTypeHasMany:
		if field.Kind() != reflect.Slice {
			return fmt.Errorf("rain: relation %q must target a slice field", relation.Name)
		}
	default:
		return fmt.Errorf("rain: unsupported relation type %q", relation.Type)
	}
	return nil
}

func (q *SelectQuery) relationElementType(parent reflect.Value, relation schema.RelationDef) (reflect.Type, error) {
	meta, _, err := lookupModelMeta(parent.Addr().Interface())
	if err != nil {
		return nil, err
	}
	fieldInfo := meta.byRelation[relation.Name]
	field := parent.FieldByIndex(fieldInfo.index)
	switch relation.Type {
	case schema.RelationTypeBelongsTo:
		if field.Kind() == reflect.Pointer {
			return field.Type().Elem(), nil
		}
		return field.Type(), nil
	case schema.RelationTypeHasMany:
		return field.Type().Elem(), nil
	default:
		return nil, fmt.Errorf("rain: unsupported relation type %q", relation.Type)
	}
}

func relationColumnValue(model reflect.Value, columnName string) (any, bool, error) {
	meta, _, err := lookupModelMeta(model.Addr().Interface())
	if err != nil {
		return nil, false, err
	}
	fieldInfo, ok := meta.byColumn[columnName]
	if !ok {
		return nil, false, nil
	}
	field := model.FieldByIndex(fieldInfo.index)
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return nil, false, nil
		}
		return field.Elem().Interface(), true, nil
	}
	return field.Interface(), true, nil
}

func setRelationValue(parent reflect.Value, relationName string, relationType schema.RelationType, matches []reflect.Value) error {
	meta, _, err := lookupModelMeta(parent.Addr().Interface())
	if err != nil {
		return err
	}
	fieldInfo := meta.byRelation[relationName]
	field := parent.FieldByIndex(fieldInfo.index)

	switch relationType {
	case schema.RelationTypeBelongsTo:
		if len(matches) == 0 {
			return nil
		}
		item := matches[0]
		if field.Kind() == reflect.Pointer {
			ptr := reflect.New(field.Type().Elem())
			ptr.Elem().Set(item)
			field.Set(ptr)
			return nil
		}
		field.Set(item)
		return nil
	case schema.RelationTypeHasMany:
		slice := reflect.MakeSlice(field.Type(), 0, len(matches))
		for _, match := range matches {
			slice = reflect.Append(slice, match)
		}
		field.Set(slice)
		return nil
	default:
		return fmt.Errorf("rain: unsupported relation type %q", relationType)
	}
}

func toTypedKey(value any) typedKey {
	return typedKey{typeName: fmt.Sprintf("%T", value), value: fmt.Sprint(value)}
}
