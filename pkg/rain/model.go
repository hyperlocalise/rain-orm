package rain

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

type modelField struct {
	index []int
}

type modelMeta struct {
	byColumn   map[string]modelField
	byRelation map[string]modelField
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

	typ := value.Type()
	if cached, ok := modelMetaCache.Load(typ); ok {
		return cached.(*modelMeta), value, nil
	}

	meta := &modelMeta{
		byColumn:   make(map[string]modelField, typ.NumField()),
		byRelation: make(map[string]modelField, typ.NumField()),
	}
	buildModelMeta(meta, typ, nil)
	actual, _ := modelMetaCache.LoadOrStore(typ, meta)

	return actual.(*modelMeta), value, nil
}

func buildModelMeta(meta *modelMeta, typ reflect.Type, prefix []int) {
	for fieldIndex := range typ.NumField() {
		field := typ.Field(fieldIndex)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}

		current := append(append([]int{}, prefix...), fieldIndex)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			buildModelMeta(meta, field.Type, current)
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
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return sql.ErrNoRows
		}
		if err := scanCurrentRow(rows, target); err != nil {
			return err
		}
		return rows.Err()
	case reflect.Slice:
		elemType := target.Type().Elem()
		for rows.Next() {
			elemPtr := reflect.New(elemType)
			elemValue := elemPtr.Elem()
			if err := scanCurrentRow(rows, elemValue); err != nil {
				return err
			}
			target.Set(reflect.Append(target, elemValue))
		}
		return rows.Err()
	default:
		return fmt.Errorf("rain: destination must point to a struct or slice")
	}
}

func scanCurrentRow(rows *sql.Rows, target reflect.Value) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	meta, _, err := lookupModelMeta(target.Addr().Interface())
	if err != nil {
		return err
	}

	targets := make([]any, len(cols))
	finalizers := make([]func() error, len(cols))
	for idx, name := range cols {
		fieldInfo, ok := meta.byColumn[name]
		if !ok {
			var discard any
			targets[idx] = &discard
			continue
		}

		field := target.FieldByIndex(fieldInfo.index)
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

func prepareScanTarget(field reflect.Value) (any, func() error, error) {
	if field.Kind() != reflect.Pointer {
		if !field.CanAddr() {
			return nil, nil, fmt.Errorf("rain: field %s is not addressable", field.Type())
		}
		return field.Addr().Interface(), nil, nil
	}

	elemType := field.Type().Elem()
	switch {
	case elemType.Kind() == reflect.String:
		holder := sql.Null[string]{}
		return &holder, func() error {
			if !holder.Valid {
				field.Set(reflect.Zero(field.Type()))
				return nil
			}
			value := holder.V
			field.Set(reflect.ValueOf(&value))
			return nil
		}, nil
	case elemType.Kind() == reflect.Int || elemType.Kind() == reflect.Int64:
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
		}, nil
	case elemType.Kind() == reflect.Bool:
		holder := sql.Null[bool]{}
		return &holder, func() error {
			if !holder.Valid {
				field.Set(reflect.Zero(field.Type()))
				return nil
			}
			value := holder.V
			field.Set(reflect.ValueOf(&value))
			return nil
		}, nil
	case elemType == reflect.TypeFor[time.Time]():
		holder := sql.Null[time.Time]{}
		return &holder, func() error {
			if !holder.Valid {
				field.Set(reflect.Zero(field.Type()))
				return nil
			}
			value := holder.V
			field.Set(reflect.ValueOf(&value))
			return nil
		}, nil
	default:
		return nil, nil, fmt.Errorf("rain: unsupported nullable field type %s", field.Type())
	}
}
