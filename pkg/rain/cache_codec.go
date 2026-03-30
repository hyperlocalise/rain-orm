package rain

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type cachedPayload struct {
	Kind   string            `json:"kind"`
	Select *cachedSelectRows `json:"select,omitempty"`
	Int64  *int64            `json:"int64,omitempty"`
	Bool   *bool             `json:"bool,omitempty"`
}

type cachedSelectRows struct {
	Columns []string        `json:"columns"`
	Rows    [][]cachedValue `json:"rows"`
}

type cachedValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

func encodeCachedSelectRows(value *cachedSelectRows) ([]byte, error) {
	payload := cachedPayload{Kind: "select", Select: value}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rain: encode cached select rows: %w", err)
	}
	return data, nil
}

func decodeCachedSelectRows(data []byte) (*cachedSelectRows, error) {
	var payload cachedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("rain: decode cached payload: %w", err)
	}
	if payload.Kind != "select" || payload.Select == nil {
		return nil, fmt.Errorf("rain: cached payload kind %q is not a select result", payload.Kind)
	}
	return payload.Select, nil
}

func encodeCachedInt64(value int64) ([]byte, error) {
	payload := cachedPayload{Kind: "int64", Int64: &value}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rain: encode cached int64: %w", err)
	}
	return data, nil
}

func decodeCachedInt64(data []byte) (int64, error) {
	var payload cachedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, fmt.Errorf("rain: decode cached payload: %w", err)
	}
	if payload.Kind != "int64" || payload.Int64 == nil {
		return 0, fmt.Errorf("rain: cached payload kind %q is not an int64 result", payload.Kind)
	}
	return *payload.Int64, nil
}

func encodeCachedBool(value bool) ([]byte, error) {
	payload := cachedPayload{Kind: "bool", Bool: &value}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("rain: encode cached bool: %w", err)
	}
	return data, nil
}

func decodeCachedBool(data []byte) (bool, error) {
	var payload cachedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return false, fmt.Errorf("rain: decode cached payload: %w", err)
	}
	if payload.Kind != "bool" || payload.Bool == nil {
		return false, fmt.Errorf("rain: cached payload kind %q is not a bool result", payload.Kind)
	}
	return *payload.Bool, nil
}

func encodeCachedValue(value any) (cachedValue, error) {
	switch item := value.(type) {
	case nil:
		return cachedValue{Kind: "null"}, nil
	case bool:
		return cachedValue{Kind: "bool", Value: strconv.FormatBool(item)}, nil
	case int64:
		return cachedValue{Kind: "int64", Value: strconv.FormatInt(item, 10)}, nil
	case float64:
		return cachedValue{Kind: "float64", Value: strconv.FormatFloat(item, 'g', -1, 64)}, nil
	case string:
		return cachedValue{Kind: "string", Value: item}, nil
	case []byte:
		return cachedValue{Kind: "bytes", Value: base64.StdEncoding.EncodeToString(item)}, nil
	case time.Time:
		return cachedValue{Kind: "time", Value: item.UTC().Format(time.RFC3339Nano)}, nil
	default:
		return cachedValue{}, fmt.Errorf("rain: unsupported cached value type %T", value)
	}
}

func decodeCachedValue(value cachedValue, column *schema.ColumnDef) (any, error) {
	switch value.Kind {
	case "null":
		return nil, nil
	case "bool":
		parsed, err := strconv.ParseBool(value.Value)
		if err != nil {
			return nil, fmt.Errorf("rain: decode cached bool: %w", err)
		}
		return parsed, nil
	case "int64":
		parsed, err := strconv.ParseInt(value.Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rain: decode cached int64: %w", err)
		}
		return parsed, nil
	case "float64":
		parsed, err := strconv.ParseFloat(value.Value, 64)
		if err != nil {
			return nil, fmt.Errorf("rain: decode cached float64: %w", err)
		}
		return parsed, nil
	case "string":
		if column != nil && (column.Type.DataType == schema.TypeJSON || column.Type.DataType == schema.TypeJSONB) {
			return []byte(value.Value), nil
		}
		return value.Value, nil
	case "bytes":
		decoded, err := base64.StdEncoding.DecodeString(value.Value)
		if err != nil {
			return nil, fmt.Errorf("rain: decode cached bytes: %w", err)
		}
		return decoded, nil
	case "time":
		parsed, err := time.Parse(time.RFC3339Nano, value.Value)
		if err != nil {
			return nil, fmt.Errorf("rain: decode cached time: %w", err)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("rain: unsupported cached value kind %q", value.Kind)
	}
}
