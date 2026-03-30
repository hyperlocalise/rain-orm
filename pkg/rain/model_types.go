package rain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
)

// Set allows value types to opt into omission semantics during writes.
type Set[T any] struct {
	Value T
	Valid bool
}

func (s Set[T]) rainSetValue() (any, bool) {
	return s.Value, s.Valid
}

func (s Set[T]) rainSetType() reflect.Type {
	return reflect.TypeFor[T]()
}

type setValueProvider interface {
	rainSetValue() (any, bool)
}

// JSON wraps a typed Go value for JSON/JSONB columns.
type JSON[T any] struct {
	V T
}

// Scan implements sql.Scanner.
func (j *JSON[T]) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		var zero T
		j.V = zero
		return nil
	case []byte:
		if len(value) == 0 {
			var zero T
			j.V = zero
			return nil
		}
		return json.Unmarshal(value, &j.V)
	case string:
		if value == "" {
			var zero T
			j.V = zero
			return nil
		}
		return json.Unmarshal([]byte(value), &j.V)
	default:
		return fmt.Errorf("rain: unsupported JSON source %T", src)
	}
}

// Value implements driver.Valuer.
func (j JSON[T]) Value() (driver.Value, error) {
	data, err := json.Marshal(j.V)
	if err != nil {
		return nil, err
	}
	return data, nil
}
