package migrator

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

const snapshotVersion = 1

// Snapshot captures a deterministic schema view and the SQL fragments needed for additive diffs.
type Snapshot struct {
	Version int             `json:"version"`
	Dialect string          `json:"dialect"`
	Tables  []TableSnapshot `json:"tables"`
}

// TableSnapshot stores a portable, deterministic representation of one table.
type TableSnapshot struct {
	Name           string               `json:"name"`
	CreateTableSQL string               `json:"create_table_sql"`
	Columns        []ColumnSnapshot     `json:"columns"`
	Constraints    []ConstraintSnapshot `json:"constraints"`
	ForeignKeys    []ForeignKeySnapshot `json:"foreign_keys"`
	Indexes        []IndexSnapshot      `json:"indexes"`
}

// ColumnSnapshot stores one column definition and its additive DDL fragment.
type ColumnSnapshot struct {
	Name          string            `json:"name"`
	Type          schema.ColumnType `json:"type"`
	Nullable      bool              `json:"nullable"`
	DefaultSQL    string            `json:"default_sql,omitempty"`
	HasDefault    bool              `json:"has_default"`
	PrimaryKey    bool              `json:"primary_key"`
	AutoIncrement bool              `json:"auto_increment"`
	Unique        bool              `json:"unique"`
	DefinitionSQL string            `json:"definition_sql"`
}

// ConstraintSnapshot stores one table-level constraint.
type ConstraintSnapshot struct {
	Name string `json:"name"`
	Type string `json:"type"`
	SQL  string `json:"sql"`
}

// ForeignKeySnapshot stores one single-column foreign key.
type ForeignKeySnapshot struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

// IndexSnapshot stores one standalone index.
type IndexSnapshot struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
}

// BuildSnapshot compiles managed tables into a deterministic snapshot.
func BuildSnapshot(dialectName string, tables []schema.TableReference) (Snapshot, error) {
	ddl, err := rain.OpenDialect(dialectName)
	if err != nil {
		return Snapshot{}, err
	}

	cloned := slices.Clone(tables)
	slices.SortFunc(cloned, func(a, b schema.TableReference) int {
		return compareStrings(a.TableDef().Name, b.TableDef().Name)
	})

	tableSnapshots := make([]TableSnapshot, 0, len(cloned))
	seenTables := make(map[string]struct{}, len(cloned))
	for _, table := range cloned {
		if table == nil || table.TableDef() == nil {
			return Snapshot{}, fmt.Errorf("migrator: managed tables must be non-nil")
		}
		tableDef := table.TableDef()
		if _, exists := seenTables[tableDef.Name]; exists {
			return Snapshot{}, fmt.Errorf("migrator: duplicate table %q in registry", tableDef.Name)
		}
		seenTables[tableDef.Name] = struct{}{}

		createTableSQL, createErr := ddl.CreateTableSQL(table)
		if createErr != nil {
			return Snapshot{}, createErr
		}
		indexSQL, indexErr := ddl.CreateIndexesSQL(table)
		if indexErr != nil {
			return Snapshot{}, indexErr
		}

		columnSnapshots := make([]ColumnSnapshot, 0, len(tableDef.Columns))
		for _, column := range tableDef.Columns {
			definitionSQL, definitionErr := ddl.ColumnDefinitionSQL(table, column.Name)
			if definitionErr != nil {
				return Snapshot{}, definitionErr
			}
			defaultSQL, defaultErr := ddl.ColumnDefaultSQL(table, column.Name)
			if defaultErr != nil {
				return Snapshot{}, defaultErr
			}
			columnSnapshots = append(columnSnapshots, ColumnSnapshot{
				Name:          column.Name,
				Type:          column.Type,
				Nullable:      column.Nullable,
				DefaultSQL:    defaultSQL,
				HasDefault:    column.HasDefault || column.DefaultSQL != "",
				PrimaryKey:    column.PrimaryKey,
				AutoIncrement: column.AutoIncrement,
				Unique:        column.Unique,
				DefinitionSQL: definitionSQL,
			})
		}

		constraintSnapshots := make([]ConstraintSnapshot, 0, len(tableDef.Constraints))
		for _, constraint := range tableDef.Constraints {
			constraintSQL, constraintErr := ddl.AddConstraintSQL(table, constraint.Name)
			if constraintErr != nil {
				return Snapshot{}, constraintErr
			}
			constraintSnapshots = append(constraintSnapshots, ConstraintSnapshot{
				Name: constraint.Name,
				Type: string(constraint.Type),
				SQL:  constraintSQL,
			})
		}
		slices.SortFunc(constraintSnapshots, func(a, b ConstraintSnapshot) int {
			return compareStrings(a.Name, b.Name)
		})

		foreignKeySnapshots := make([]ForeignKeySnapshot, 0, len(tableDef.ForeignKeys))
		for _, foreignKey := range tableDef.ForeignKeys {
			if foreignKey.Name == "" {
				continue
			}
			foreignKeySQL, foreignKeyErr := ddl.AddForeignKeySQL(table, foreignKey.Name)
			if foreignKeyErr != nil {
				return Snapshot{}, foreignKeyErr
			}
			foreignKeySnapshots = append(foreignKeySnapshots, ForeignKeySnapshot{
				Name: foreignKey.Name,
				SQL:  foreignKeySQL,
			})
		}
		slices.SortFunc(foreignKeySnapshots, func(a, b ForeignKeySnapshot) int {
			return compareStrings(a.Name, b.Name)
		})

		indexSnapshots := make([]IndexSnapshot, 0, len(indexSQL))
		for idx, statement := range indexSQL {
			indexSnapshots = append(indexSnapshots, IndexSnapshot{
				Name: tableDef.Indexes[idx].Name,
				SQL:  statement,
			})
		}
		slices.SortFunc(indexSnapshots, func(a, b IndexSnapshot) int {
			return compareStrings(a.Name, b.Name)
		})

		tableSnapshots = append(tableSnapshots, TableSnapshot{
			Name:           tableDef.Name,
			CreateTableSQL: createTableSQL,
			Columns:        columnSnapshots,
			Constraints:    constraintSnapshots,
			ForeignKeys:    foreignKeySnapshots,
			Indexes:        indexSnapshots,
		})
	}

	return Snapshot{
		Version: snapshotVersion,
		Dialect: dialectName,
		Tables:  tableSnapshots,
	}, nil
}

// MarshalSnapshot produces stable, indented JSON for disk storage.
func MarshalSnapshot(snapshot Snapshot) ([]byte, error) {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(data, '\n'), nil
}

// UnmarshalSnapshot parses one JSON snapshot file.
func UnmarshalSnapshot(data []byte) (Snapshot, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	if snapshot.Version != snapshotVersion {
		return Snapshot{}, fmt.Errorf("migrator: unsupported snapshot version %d", snapshot.Version)
	}

	return snapshot, nil
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
