package rain

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type tableModelBindingKey struct {
	table     *schema.TableDef
	modelType reflect.Type
	strict    bool
}

type tableModelBinding struct {
	meta *modelMeta
	err  error
}

var tableModelBindingCache sync.Map

type setTypeProvider interface {
	rainSetType() reflect.Type
}

// BindTableModel validates a full table-backed read/write model type against a table and caches the result.
func BindTableModel[T any](table schema.TableReference) error {
	typ, err := structTypeForType(reflect.TypeFor[T]())
	if err != nil {
		return err
	}
	_, err = lookupTableModelBinding(typ, table.TableDef(), true)
	return err
}

// MustBindTableModel validates a full table-backed read/write model type and panics on error.
func MustBindTableModel[T any](table schema.TableReference) {
	if err := BindTableModel[T](table); err != nil {
		panic(err)
	}
}

// BindModel validates a model type against a table and caches the result.
// Deprecated: use BindTableModel for strict table-backed read/write models.
func BindModel[T any](table schema.TableReference) error {
	return BindTableModel[T](table)
}

// MustBindModel validates a model type against a table and panics on error.
// Deprecated: use MustBindTableModel for strict table-backed read/write models.
func MustBindModel[T any](table schema.TableReference) {
	MustBindTableModel[T](table)
}

func lookupTableModelBinding(typ reflect.Type, table *schema.TableDef, strict bool) (*tableModelBinding, error) {
	if table == nil {
		return nil, fmt.Errorf("rain: model binding requires a non-nil table")
	}

	modelType, err := structTypeForType(typ)
	if err != nil {
		return nil, err
	}

	key := tableModelBindingKey{table: table, modelType: modelType, strict: strict}
	if cached, ok := tableModelBindingCache.Load(key); ok {
		binding := cached.(*tableModelBinding)
		return binding, binding.err
	}

	meta, err := lookupModelMetaForType(modelType)
	if err == nil {
		err = validateModelMetaAgainstTable(meta, modelType, table, strict)
	}
	binding := &tableModelBinding{meta: meta, err: err}
	actual, _ := tableModelBindingCache.LoadOrStore(key, binding)
	resolved := actual.(*tableModelBinding)
	return resolved, resolved.err
}

func structTypeForType(typ reflect.Type) (reflect.Type, error) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("rain: model must be a struct or pointer to struct")
	}
	return typ, nil
}

func validateModelMetaAgainstTable(meta *modelMeta, modelType reflect.Type, table *schema.TableDef, strict bool) error {
	var validationErrors []error

	for columnName, field := range meta.byColumn {
		column, ok := table.ColumnByName(columnName)
		if !ok {
			if strict {
				validationErrors = append(validationErrors, fmt.Errorf("rain: model field mapped to unknown column %q on table %q", columnName, table.Name))
			}
			continue
		}
		if err := validateModelFieldCompatibility(column, modelType.FieldByIndex(field.index).Type); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}

	for relationName := range meta.byRelation {
		if _, ok := table.RelationByName(relationName); !ok && strict {
			validationErrors = append(validationErrors, fmt.Errorf("rain: model field mapped to unknown relation %q on table %q", relationName, table.Name))
		}
	}

	return joinValidationErrors(validationErrors)
}

func validateScanColumnsAgainstTable(modelType reflect.Type, table *schema.TableDef, columns []string) error {
	if table == nil {
		return nil
	}

	meta, err := lookupModelMetaForType(modelType)
	if err != nil {
		return err
	}

	var validationErrors []error
	for _, name := range columns {
		field, ok := meta.byColumn[name]
		if !ok {
			continue
		}
		column, exists := table.ColumnByName(name)
		if !exists {
			if !field.explicitColumn {
				validationErrors = append(validationErrors, fmt.Errorf("rain: selected column %q does not exist on table %q", name, table.Name))
			}
			continue
		}
		if err := validateScanCompatibility(column, modelType.FieldByIndex(field.index).Type); err != nil {
			validationErrors = append(validationErrors, err)
		}
	}

	return joinValidationErrors(validationErrors)
}

func joinValidationErrors(errs []error) error {
	errs = slices.DeleteFunc(errs, func(err error) bool { return err == nil })
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		var parts []string
		for _, err := range errs {
			parts = append(parts, err.Error())
		}
		return errors.New(strings.Join(parts, "; "))
	}
}

func validateModelFieldCompatibility(column *schema.ColumnDef, fieldType reflect.Type) error {
	if err := validateScanCompatibility(column, fieldType); err != nil {
		return err
	}
	if err := validateWriteCompatibility(column, fieldType); err != nil {
		return err
	}
	return nil
}

func validateScanCompatibility(column *schema.ColumnDef, fieldType reflect.Type) error {
	if supportsScanForColumn(column, fieldType) {
		return nil
	}
	return fmt.Errorf("rain: field type %s is not compatible with scan column %q (%s)", fieldType, column.Name, column.Type.DataType)
}

func validateWriteCompatibility(column *schema.ColumnDef, fieldType reflect.Type) error {
	if supportsWriteForColumn(column, fieldType) {
		return nil
	}
	return fmt.Errorf("rain: field type %s is not compatible with write column %q (%s)", fieldType, column.Name, column.Type.DataType)
}

func supportsScanForColumn(column *schema.ColumnDef, fieldType reflect.Type) bool {
	if fieldType == nil {
		return false
	}
	if fieldType.Kind() == reflect.Interface && fieldType.NumMethod() == 0 {
		return true
	}
	if supportsScanner(fieldType) {
		return true
	}

	baseType, _ := unwrapFieldType(fieldType)

	switch column.Type.DataType {
	case schema.TypeSmallSerial, schema.TypeSerial, schema.TypeBigSerial, schema.TypeSmallInt, schema.TypeInteger, schema.TypeBigInt:
		return isIntegerKind(baseType.Kind())
	case schema.TypeReal, schema.TypeDouble:
		return baseType.Kind() == reflect.Float32 || baseType.Kind() == reflect.Float64
	case schema.TypeDecimal:
		return baseType.Kind() == reflect.String
	case schema.TypeText, schema.TypeVarChar, schema.TypeUUID, schema.TypeEnum:
		return baseType.Kind() == reflect.String
	case schema.TypeBoolean:
		return baseType.Kind() == reflect.Bool
	case schema.TypeBytes:
		return isBytesType(baseType)
	case schema.TypeDate, schema.TypeTimestamp, schema.TypeTimestampTZ:
		return baseType == reflect.TypeFor[time.Time]() || baseType.Kind() == reflect.String || isBytesType(baseType)
	case schema.TypeJSON, schema.TypeJSONB:
		return isJSONCompatibleType(baseType) || supportsScanner(fieldType)
	default:
		return false
	}
}

func supportsWriteForColumn(column *schema.ColumnDef, fieldType reflect.Type) bool {
	if fieldType == nil {
		return false
	}
	if fieldType.Kind() == reflect.Interface && fieldType.NumMethod() == 0 {
		return true
	}
	if supportsValuer(fieldType) || fieldType.Implements(reflect.TypeFor[schema.Expression]()) {
		return true
	}

	baseType, _ := unwrapFieldType(fieldType)

	switch column.Type.DataType {
	case schema.TypeSmallSerial, schema.TypeSerial, schema.TypeBigSerial, schema.TypeSmallInt, schema.TypeInteger, schema.TypeBigInt:
		return isIntegerKind(baseType.Kind())
	case schema.TypeReal, schema.TypeDouble:
		return baseType.Kind() == reflect.Float32 || baseType.Kind() == reflect.Float64
	case schema.TypeDecimal:
		return baseType.Kind() == reflect.String
	case schema.TypeText, schema.TypeVarChar, schema.TypeUUID, schema.TypeEnum:
		return baseType.Kind() == reflect.String
	case schema.TypeBoolean:
		return baseType.Kind() == reflect.Bool
	case schema.TypeBytes:
		return isBytesType(baseType)
	case schema.TypeDate, schema.TypeTimestamp, schema.TypeTimestampTZ:
		return baseType == reflect.TypeFor[time.Time]()
	case schema.TypeJSON, schema.TypeJSONB:
		return isJSONCompatibleType(baseType) || supportsValuer(fieldType)
	default:
		return false
	}
}

func unwrapFieldType(fieldType reflect.Type) (reflect.Type, bool) {
	current := fieldType
	explicit := false
	for current.Kind() == reflect.Pointer {
		current = current.Elem()
		explicit = true
	}

	if setType, ok := extractSetFieldType(current); ok {
		current = setType
		explicit = true
		for current.Kind() == reflect.Pointer {
			current = current.Elem()
		}
	}

	return current, explicit
}

func extractSetFieldType(fieldType reflect.Type) (reflect.Type, bool) {
	providerType := reflect.TypeFor[setTypeProvider]()
	switch {
	case fieldType.Implements(providerType):
		return reflect.Zero(fieldType).Interface().(setTypeProvider).rainSetType(), true
	case reflect.PointerTo(fieldType).Implements(providerType):
		return reflect.New(fieldType).Interface().(setTypeProvider).rainSetType(), true
	default:
		return nil, false
	}
}

func supportsScanner(fieldType reflect.Type) bool {
	if fieldType == nil {
		return false
	}
	scannerType := reflect.TypeFor[sql.Scanner]()
	return fieldType.Implements(scannerType) || reflect.PointerTo(fieldType).Implements(scannerType)
}

func supportsValuer(fieldType reflect.Type) bool {
	if fieldType == nil {
		return false
	}
	valuerType := reflect.TypeFor[driver.Valuer]()
	return fieldType.Implements(valuerType) || reflect.PointerTo(fieldType).Implements(valuerType)
}

func isIntegerKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

func isBytesType(typ reflect.Type) bool {
	return typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8
}

func isJSONCompatibleType(typ reflect.Type) bool {
	if typ == reflect.TypeFor[json.RawMessage]() {
		return true
	}
	if typ.Kind() == reflect.String {
		return true
	}
	if isBytesType(typ) {
		return true
	}
	return supportsScanner(typ) && supportsValuer(typ)
}
