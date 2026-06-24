package rain

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

const relationBatchSize = 512

type typedKey struct {
	typ   reflect.Type
	value any
}

type relationLoadNode struct {
	name     string
	relation schema.RelationDef
	config   RelationConfig
	children map[string]*relationLoadNode
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

	if q.table == nil {
		return fmt.Errorf("rain: relation loading requires a concrete table source")
	}

	relationTree, err := buildRelationLoadTree(q.table, q.relationNames, q.relationConfigs)
	if err != nil {
		return err
	}

	containerPtr := dest
	if isSingle {
		sliceType := reflect.SliceOf(target.Type())
		slicePtr := reflect.New(sliceType)
		containerPtr = slicePtr.Interface()
	}

	if err := scanRowsAgainstTable(rows, containerPtr, q.table); err != nil {
		return err
	}

	sliceValue := reflect.ValueOf(containerPtr).Elem()
	if err := q.loadRelationsIntoSlice(ctx, sliceValue, relationTree); err != nil {
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

func buildRelationLoadTree(table *schema.TableDef, relationNames []string, relationConfigs map[string]RelationConfig) (map[string]*relationLoadNode, error) {
	tree := make(map[string]*relationLoadNode, len(relationNames))
	for _, rawName := range relationNames {
		parts := strings.Split(rawName, ".")
		currentTable := table
		currentLevel := tree
		var currentPath strings.Builder
		for idx, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return nil, fmt.Errorf("rain: relation path %q contains an empty segment", rawName)
			}
			if idx > 0 {
				currentPath.WriteByte('.')
			}
			currentPath.WriteString(part)

			relation, exists := currentTable.RelationByName(part)
			if !exists {
				return nil, fmt.Errorf("rain: unknown relation %q on table %q", part, currentTable.Name)
			}
			node, exists := currentLevel[part]
			if !exists {
				node = &relationLoadNode{
					name:     part,
					relation: relation,
					children: make(map[string]*relationLoadNode),
				}
				currentLevel[part] = node
			}
			if relationConfigs != nil {
				if cfg, ok := relationConfigs[currentPath.String()]; ok {
					node.config = cfg
				}
			}
			currentTable = relation.TargetTable
			currentLevel = node.children
		}
	}
	return tree, nil
}

func (q *SelectQuery) loadRelationsIntoSlice(
	ctx context.Context,
	parents reflect.Value,
	relationTree map[string]*relationLoadNode,
) error {
	if parents.Len() == 0 || len(relationTree) == 0 {
		return nil
	}

	for _, node := range relationTree {
		if err := q.loadRelationNode(ctx, parents, node); err != nil {
			return err
		}
	}

	return nil
}

func (q *SelectQuery) loadRelationNode(ctx context.Context, parents reflect.Value, node *relationLoadNode) error {
	var (
		parentMeta        *modelMeta
		sourceColumnIndex []int
		relationIndex     []int
	)
	if parents.Len() > 0 {
		// Validate the relation field once for the entire batch and resolve metadata.
		for idx := 0; idx < parents.Len(); idx++ {
			parent := dereferenceModelValue(parents.Index(idx))
			if !parent.IsValid() {
				continue
			}
			var err error
			parentMeta, err = lookupModelMetaForType(parent.Type())
			if err != nil {
				return err
			}
			if info, ok := parentMeta.byColumn[node.relation.SourceColumn.Name]; ok {
				sourceColumnIndex = info.index
			}
			if info, ok := parentMeta.byRelation[node.name]; ok {
				relationIndex = info.index
			}

			if err := q.validateRelationField(parent, node.relation, parentMeta); err != nil {
				return err
			}
			break
		}
	}

	sourceKeys := make(map[typedKey]any, parents.Len())
	orderedSourceKeys := make([]any, 0, parents.Len())
	for idx := 0; idx < parents.Len(); idx++ {
		parent := dereferenceModelValue(parents.Index(idx))
		if !parent.IsValid() {
			continue
		}
		keyValue, ok, err := relationColumnValue(parent, node.relation.SourceColumn.Name, parentMeta, sourceColumnIndex)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		key := toTypedKey(keyValue)
		if _, exists := sourceKeys[key]; exists {
			continue
		}
		sourceKeys[key] = keyValue
		orderedSourceKeys = append(orderedSourceKeys, keyValue)
	}
	if len(sourceKeys) == 0 {
		return nil
	}

	var relatedRows reflect.Value
	var relatedBySourceKey map[typedKey][]reflect.Value

	if node.relation.Type == schema.RelationTypeManyToMany {
		var err error
		relatedRows, relatedBySourceKey, err = q.loadRelatedManyToManyRows(ctx, parents, node.relation, node.config, orderedSourceKeys, node)
		if err != nil {
			return err
		}
	} else {
		var err error
		relatedRows, err = q.loadRelatedRows(ctx, parents, node.relation, node.config, orderedSourceKeys, node)
		if err != nil {
			return err
		}

		relatedBySourceKey = make(map[typedKey][]reflect.Value, len(sourceKeys))
		var (
			relatedMeta       *modelMeta
			targetColumnIndex []int
		)
		for rowIdx := 0; rowIdx < relatedRows.Len(); rowIdx++ {
			related := dereferenceModelValue(relatedRows.Index(rowIdx))
			if !related.IsValid() {
				continue
			}
			if relatedMeta == nil {
				var err error
				relatedMeta, err = lookupModelMetaForType(related.Type())
				if err != nil {
					return err
				}
				if info, ok := relatedMeta.byColumn[node.relation.TargetColumn.Name]; ok {
					targetColumnIndex = info.index
				}
			}
			targetValue, ok, err := relationColumnValue(related, node.relation.TargetColumn.Name, relatedMeta, targetColumnIndex)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			relatedBySourceKey[toTypedKey(targetValue)] = append(relatedBySourceKey[toTypedKey(targetValue)], related)
		}
	}

	if len(node.children) > 0 {
		if err := q.loadRelationsIntoSlice(ctx, relatedRows, node.children); err != nil {
			return err
		}
	}

	for idx := 0; idx < parents.Len(); idx++ {
		parent := dereferenceModelValue(parents.Index(idx))
		if !parent.IsValid() {
			continue
		}
		sourceValue, ok, err := relationColumnValue(parent, node.relation.SourceColumn.Name, parentMeta, sourceColumnIndex)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		matches := relatedBySourceKey[toTypedKey(sourceValue)]
		if err := setRelationValue(parent, node.name, node.relation.Type, matches, parentMeta, relationIndex); err != nil {
			return err
		}
	}

	return nil
}

func (q *SelectQuery) loadRelatedManyToManyRows(
	ctx context.Context,
	parents reflect.Value,
	relation schema.RelationDef,
	config RelationConfig,
	sourceKeys []any,
	node *relationLoadNode,
) (reflect.Value, map[typedKey][]reflect.Value, error) {
	parentStructType, err := sliceParentStructType(parents.Type())
	if err != nil {
		return reflect.Value{}, nil, err
	}
	relatedElemType, err := q.relationElementTypeFromType(parentStructType, relation)
	if err != nil {
		return reflect.Value{}, nil, err
	}

	type pair struct {
		S any `db:"s"`
		T any `db:"t"`
	}
	var allPairs []pair

	// Step 1: Collect all (sourceKey, targetKey) pairs from join table
	for start := 0; start < len(sourceKeys); start += relationBatchSize {
		end := min(start+relationBatchSize, len(sourceKeys))
		batchKeys := sourceKeys[start:end]

		var batchPairs []pair
		joinQuery := &SelectQuery{runner: q.runner, dialect: q.dialect, table: relation.JoinTable}
		if err := joinQuery.
			Column(schema.Ref(relation.JoinSourceColumn).As("s"), schema.Ref(relation.JoinTargetColumn).As("t")).
			Where(schema.Ref(relation.JoinSourceColumn).In(batchKeys...)).
			Scan(ctx, &batchPairs); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return reflect.Value{}, nil, err
		}
		allPairs = append(allPairs, batchPairs...)
	}

	if len(allPairs) == 0 {
		return reflect.MakeSlice(reflect.SliceOf(relatedElemType), 0, 0), make(map[typedKey][]reflect.Value), nil
	}

	// Step 2: Collect unique target keys
	targetKeyMap := make(map[typedKey]any)
	var uniqueTargetKeys []any
	for _, p := range allPairs {
		tKey := toTypedKey(p.T)
		if _, ok := targetKeyMap[tKey]; !ok {
			targetKeyMap[tKey] = p.T
			uniqueTargetKeys = append(uniqueTargetKeys, p.T)
		}
	}

	// Step 3: Fetch unique target rows
	targetRowsMap := make(map[typedKey]reflect.Value)
	relatedRows := reflect.MakeSlice(reflect.SliceOf(relatedElemType), 0, len(uniqueTargetKeys))

	for start := 0; start < len(uniqueTargetKeys); start += relationBatchSize {
		end := min(start+relationBatchSize, len(uniqueTargetKeys))
		batchKeys := uniqueTargetKeys[start:end]

		batchDest := reflect.New(reflect.SliceOf(relatedElemType))
		targetQuery := &SelectQuery{runner: q.runner, dialect: q.dialect, table: relation.TargetTable}
		if len(config.Columns) > 0 {
			targetQuery.Column(config.Columns...)
			ensureTargetColumnSelected(targetQuery, relation.TargetColumn)
			for _, child := range node.children {
				ensureTargetColumnSelected(targetQuery, child.relation.SourceColumn)
			}
		}
		if config.Where != nil {
			targetQuery.Where(config.Where)
		}
		if len(config.OrderBy) > 0 {
			targetQuery.OrderBy(config.OrderBy...)
		}
		if err := targetQuery.
			Where(schema.Ref(relation.TargetColumn).In(batchKeys...)).
			Scan(ctx, batchDest.Interface()); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return reflect.Value{}, nil, err
		}

		batchDestElem := batchDest.Elem()
		var (
			batchMeta         *modelMeta
			targetColumnIndex []int
		)
		for i := 0; i < batchDestElem.Len(); i++ {
			row := dereferenceModelValue(batchDestElem.Index(i))
			if !row.IsValid() {
				continue
			}
			if batchMeta == nil {
				var err error
				batchMeta, err = lookupModelMetaForType(row.Type())
				if err != nil {
					return reflect.Value{}, nil, err
				}
				if info, ok := batchMeta.byColumn[relation.TargetColumn.Name]; ok {
					targetColumnIndex = info.index
				}
			}
			tVal, ok, err := relationColumnValue(row, relation.TargetColumn.Name, batchMeta, targetColumnIndex)
			if err != nil {
				return reflect.Value{}, nil, err
			}
			if ok {
				tKey := toTypedKey(tVal)
				targetRowsMap[tKey] = row
			}
		}
		relatedRows = reflect.AppendSlice(relatedRows, batchDestElem)
	}

	// Step 4: Map source keys to target rows, preserving the order of relatedRows.
	relatedBySourceKey := make(map[typedKey][]reflect.Value)

	// Map target key to slice of source keys from allPairs.
	sourceKeysByTargetKey := make(map[typedKey][]typedKey)
	for _, p := range allPairs {
		sKey := toTypedKey(p.S)
		tKey := toTypedKey(p.T)
		sourceKeysByTargetKey[tKey] = append(sourceKeysByTargetKey[tKey], sKey)
	}

	var (
		relatedMeta       *modelMeta
		targetColumnIndex []int
	)
	for rowIdx := 0; rowIdx < relatedRows.Len(); rowIdx++ {
		row := relatedRows.Index(rowIdx)
		deref := dereferenceModelValue(row)
		if !deref.IsValid() {
			continue
		}
		if relatedMeta == nil {
			var err error
			relatedMeta, err = lookupModelMetaForType(deref.Type())
			if err != nil {
				return reflect.Value{}, nil, err
			}
			if info, ok := relatedMeta.byColumn[relation.TargetColumn.Name]; ok {
				targetColumnIndex = info.index
			}
		}
		tVal, ok, err := relationColumnValue(deref, relation.TargetColumn.Name, relatedMeta, targetColumnIndex)
		if err != nil {
			return reflect.Value{}, nil, err
		}
		if !ok {
			continue
		}
		tKey := toTypedKey(tVal)
		for _, sKey := range sourceKeysByTargetKey[tKey] {
			relatedBySourceKey[sKey] = append(relatedBySourceKey[sKey], row)
		}
	}

	return relatedRows, relatedBySourceKey, nil
}

func (q *SelectQuery) loadRelatedRows(
	ctx context.Context,
	parents reflect.Value,
	relation schema.RelationDef,
	config RelationConfig,
	sourceKeys []any,
	node *relationLoadNode,
) (reflect.Value, error) {
	parentStructType, err := sliceParentStructType(parents.Type())
	if err != nil {
		return reflect.Value{}, err
	}
	relatedElemType, err := q.relationElementTypeFromType(parentStructType, relation)
	if err != nil {
		return reflect.Value{}, err
	}

	relatedRows := reflect.New(reflect.SliceOf(relatedElemType))
	for start := 0; start < len(sourceKeys); start += relationBatchSize {
		end := min(start+relationBatchSize, len(sourceKeys))
		batchDest := reflect.New(reflect.SliceOf(relatedElemType))
		query := &SelectQuery{runner: q.runner, dialect: q.dialect, table: relation.TargetTable}
		if len(config.Columns) > 0 {
			query.Column(config.Columns...)
			ensureTargetColumnSelected(query, relation.TargetColumn)
			for _, child := range node.children {
				ensureTargetColumnSelected(query, child.relation.SourceColumn)
			}
		}
		if config.Where != nil {
			query.Where(config.Where)
		}
		if len(config.OrderBy) > 0 {
			query.OrderBy(config.OrderBy...)
		}
		if err := query.Where(schema.Ref(relation.TargetColumn).In(sourceKeys[start:end]...)).
			Scan(ctx, batchDest.Interface()); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return reflect.Value{}, err
		}
		relatedRows.Elem().Set(reflect.AppendSlice(relatedRows.Elem(), batchDest.Elem()))
	}

	return relatedRows.Elem(), nil
}

func (q *SelectQuery) validateRelationField(parent reflect.Value, relation schema.RelationDef, meta *modelMeta) error {
	if _, err := lookupTableModelBinding(parent.Type(), relation.SourceColumn.Table, true); err != nil {
		return err
	}
	if meta == nil {
		var err error
		meta, _, err = lookupModelMeta(parent.Addr().Interface())
		if err != nil {
			return err
		}
	}
	fieldInfo, ok := meta.byRelation[relation.Name]
	if !ok {
		return fmt.Errorf("rain: relation %q requires a struct field tagged with `rain:\"relation:%s\"`", relation.Name, relation.Name)
	}
	field, err := fieldByIndexAlloc(parent, fieldInfo.index)
	if err != nil {
		return err
	}
	switch relation.Type {
	case schema.RelationTypeBelongsTo, schema.RelationTypeHasOne:
		if field.Kind() != reflect.Struct && field.Kind() != reflect.Pointer {
			return fmt.Errorf("rain: relation %q must target a struct or pointer-to-struct field", relation.Name)
		}
	case schema.RelationTypeHasMany, schema.RelationTypeManyToMany:
		if field.Kind() != reflect.Slice {
			return fmt.Errorf("rain: relation %q must target a slice field", relation.Name)
		}
		elemType := field.Type().Elem()
		if elemType.Kind() != reflect.Struct && (elemType.Kind() != reflect.Pointer || elemType.Elem().Kind() != reflect.Struct) {
			return fmt.Errorf("rain: relation %q must target a slice of struct or pointer-to-struct", relation.Name)
		}
	default:
		return fmt.Errorf("rain: unsupported relation type %q", relation.Type)
	}
	return nil
}

func sliceParentStructType(sliceType reflect.Type) (reflect.Type, error) {
	if sliceType.Kind() != reflect.Slice {
		return nil, fmt.Errorf("rain: relation loading requires a slice, got %s", sliceType)
	}
	elemType := sliceType.Elem()
	if elemType.Kind() == reflect.Pointer {
		elemType = elemType.Elem()
	}
	if elemType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("rain: relation loading requires slice elements to be structs, got %s", sliceType.Elem())
	}
	return elemType, nil
}

func (q *SelectQuery) relationElementTypeFromType(parentType reflect.Type, relation schema.RelationDef) (reflect.Type, error) {
	if _, err := lookupTableModelBinding(parentType, relation.SourceColumn.Table, true); err != nil {
		return nil, err
	}
	meta, err := lookupModelMetaForType(parentType)
	if err != nil {
		return nil, err
	}
	fieldInfo, ok := meta.byRelation[relation.Name]
	if !ok {
		return nil, fmt.Errorf("rain: relation %q not found in model metadata", relation.Name)
	}
	fieldType := parentType.FieldByIndex(fieldInfo.index).Type
	switch relation.Type {
	case schema.RelationTypeBelongsTo, schema.RelationTypeHasOne:
		if fieldType.Kind() == reflect.Pointer {
			return fieldType.Elem(), nil
		}
		return fieldType, nil
	case schema.RelationTypeHasMany, schema.RelationTypeManyToMany:
		elemType := fieldType.Elem()
		if elemType.Kind() == reflect.Pointer {
			return elemType.Elem(), nil
		}
		return elemType, nil
	default:
		return nil, fmt.Errorf("rain: unsupported relation type %q", relation.Type)
	}
}

func relationColumnValue(model reflect.Value, columnName string, meta *modelMeta, index []int) (any, bool, error) {
	if index == nil {
		if meta == nil {
			var err error
			meta, _, err = lookupModelMeta(model.Addr().Interface())
			if err != nil {
				return nil, false, err
			}
		}
		fieldInfo, ok := meta.byColumn[columnName]
		if !ok {
			return nil, false, nil
		}
		index = fieldInfo.index
	}
	field, err := fieldByIndexAlloc(model, index)
	if err != nil {
		return nil, false, err
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return nil, false, nil
		}
		return field.Elem().Interface(), true, nil
	}
	return field.Interface(), true, nil
}

func setRelationValue(parent reflect.Value, relationName string, relationType schema.RelationType, matches []reflect.Value, meta *modelMeta, index []int) error {
	if index == nil {
		if meta == nil {
			var err error
			meta, _, err = lookupModelMeta(parent.Addr().Interface())
			if err != nil {
				return err
			}
		}
		fieldInfo, ok := meta.byRelation[relationName]
		if !ok {
			return fmt.Errorf("rain: relation %q not found in model metadata", relationName)
		}
		index = fieldInfo.index
	}
	field, err := fieldByIndexAlloc(parent, index)
	if err != nil {
		return err
	}

	switch relationType {
	case schema.RelationTypeBelongsTo, schema.RelationTypeHasOne:
		if len(matches) == 0 {
			return nil
		}
		if relationType == schema.RelationTypeHasOne && len(matches) > 1 {
			return fmt.Errorf("rain: has_one relation %q returned %d matches, expected at most 1", relationName, len(matches))
		}
		item := dereferenceModelValue(matches[0])
		if !item.IsValid() {
			return nil
		}
		if field.Kind() == reflect.Pointer {
			ptr := reflect.New(field.Type().Elem())
			ptr.Elem().Set(item)
			field.Set(ptr)
			return nil
		}
		field.Set(item)
		return nil
	case schema.RelationTypeHasMany, schema.RelationTypeManyToMany:
		slice := reflect.MakeSlice(field.Type(), 0, len(matches))
		pointerElems := field.Type().Elem().Kind() == reflect.Pointer
		for _, match := range matches {
			item := dereferenceModelValue(match)
			if !item.IsValid() {
				continue
			}
			if pointerElems {
				ptr := reflect.New(field.Type().Elem().Elem())
				ptr.Elem().Set(item)
				slice = reflect.Append(slice, ptr)
				continue
			}
			slice = reflect.Append(slice, item)
		}
		field.Set(slice)
		return nil
	default:
		return fmt.Errorf("rain: unsupported relation type %q", relationType)
	}
}

func dereferenceModelValue(value reflect.Value) reflect.Value {
	current := value
	for current.IsValid() && current.Kind() == reflect.Pointer {
		if current.IsNil() {
			return reflect.Value{}
		}
		current = current.Elem()
	}
	return current
}

func toTypedKey(value any) typedKey {
	// OPTIMIZATION: Type-switch fast-paths for common primary/foreign key types
	// to avoid reflect.TypeOf and reflect.ValueOf allocations.
	switch v := value.(type) {
	case int64:
		return typedKey{typ: reflect.TypeFor[int64](), value: v}
	case string:
		return typedKey{typ: reflect.TypeFor[string](), value: v}
	case int:
		return typedKey{typ: reflect.TypeFor[int](), value: v}
	case uint32:
		return typedKey{typ: reflect.TypeFor[uint32](), value: v}
	case int32:
		return typedKey{typ: reflect.TypeFor[int32](), value: v}
	case int16:
		return typedKey{typ: reflect.TypeFor[int16](), value: v}
	case int8:
		return typedKey{typ: reflect.TypeFor[int8](), value: v}
	case uint:
		return typedKey{typ: reflect.TypeFor[uint](), value: v}
	case uint64:
		return typedKey{typ: reflect.TypeFor[uint64](), value: v}
	case uint16:
		return typedKey{typ: reflect.TypeFor[uint16](), value: v}
	case uint8:
		return typedKey{typ: reflect.TypeFor[uint8](), value: v}
	case bool:
		return typedKey{typ: reflect.TypeFor[bool](), value: v}
	case float64:
		return typedKey{typ: reflect.TypeFor[float64](), value: v}
	case float32:
		return typedKey{typ: reflect.TypeFor[float32](), value: v}
	}

	return typedKey{typ: reflect.TypeOf(value), value: normalizeTypedKeyValue(value)}
}

func normalizeTypedKeyValue(value any) any {
	// OPTIMIZATION: Type-switch fast-paths for common comparable types.
	switch v := value.(type) {
	case int64, string, int, bool, float64, int32, uint32, uint64, int16, int8, uint, uint16, uint8, float32:
		return v
	case []byte:
		return string(v)
	}

	if value == nil {
		return nil
	}

	rv := reflect.ValueOf(value)
	if rv.Type().Comparable() {
		return value
	}

	// Fallback for uncommon non-comparable key types. Primary/foreign key values are
	// expected to be comparable primitives or []byte in normal ORM usage.
	return strconv.Quote(fmt.Sprintf("%#v", value))
}

func ensureTargetColumnSelected(q *SelectQuery, targetCol *schema.ColumnDef) {
	if len(q.cols) == 0 {
		return
	}

	for _, colExpr := range q.cols {
		if colRef, ok := colExpr.(schema.ColumnReference); ok {
			if colRef.ColumnDef() == targetCol {
				return
			}
		}

		// Also check for aliased expressions or raw SQL that might already provide the column name.
		var name string
		switch v := colExpr.(type) {
		case schema.AliasExpr:
			name = v.Alias
		case schema.RawExpr:
			name = v.SQL
		}
		if name == targetCol.Name || name == "\""+targetCol.Name+"\"" {
			return
		}
	}

	q.Column(schema.Ref(targetCol))
}
