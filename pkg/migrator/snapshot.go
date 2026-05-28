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
	IsView         bool                 `json:"is_view,omitempty"`
	CreateTableSQL string               `json:"create_table_sql"`
	Columns        []ColumnSnapshot     `json:"columns"`
	Constraints    []ConstraintSnapshot `json:"constraints"`
	ForeignKeys    []ForeignKeySnapshot `json:"foreign_keys"`
	Indexes        []IndexSnapshot      `json:"indexes"`
}

// ColumnSnapshot stores one column definition and its additive DDL fragment.
type ColumnSnapshot struct {
	Name            string            `json:"name"`
	Type            schema.ColumnType `json:"type"`
	Nullable        bool              `json:"nullable"`
	DefaultSQL      string            `json:"default_sql,omitempty"`
	HasDefault      bool              `json:"has_default"`
	PrimaryKey      bool              `json:"primary_key"`
	AutoIncrement   bool              `json:"auto_increment"`
	Unique          bool              `json:"unique"`
	GeneratedStored bool              `json:"generated_stored"`
	DefinitionSQL   string            `json:"definition_sql"`
}

// ConstraintSnapshot stores one table-level constraint.
type ConstraintSnapshot struct {
	Name string `json:"name"`
	Type string `json:"type"`
	SQL  string `json:"sql"`
}

// ForeignKeySnapshot stores one single-column foreign key.
type ForeignKeySnapshot struct {
	Name            string `json:"name"`
	ReferencedTable string `json:"referenced_table,omitempty"`
	SQL             string `json:"sql"`
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

	cloned, err := orderManagedTables(tables)
	if err != nil {
		return Snapshot{}, err
	}

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

		var columnSnapshots []ColumnSnapshot
		if !tableDef.IsView {
			columnSnapshots = make([]ColumnSnapshot, 0, len(tableDef.Columns))
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
					Name:            column.Name,
					Type:            column.Type,
					Nullable:        column.Nullable,
					DefaultSQL:      defaultSQL,
					HasDefault:      column.HasDefault || column.DefaultSQL != "",
					PrimaryKey:      column.PrimaryKey,
					AutoIncrement:   column.AutoIncrement,
					Unique:          column.Unique,
					GeneratedStored: column.GeneratedStored,
					DefinitionSQL:   definitionSQL,
				})
			}
		}

		var constraintSnapshots []ConstraintSnapshot
		if !tableDef.IsView {
			constraintSnapshots = make([]ConstraintSnapshot, 0, len(tableDef.Constraints))
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
		}

		var foreignKeySnapshots []ForeignKeySnapshot
		if !tableDef.IsView {
			foreignKeySnapshots = make([]ForeignKeySnapshot, 0, len(tableDef.ForeignKeys))
			for _, foreignKey := range tableDef.ForeignKeys {
				if foreignKey.Name == "" {
					continue
				}
				foreignKeySQL, foreignKeyErr := ddl.AddForeignKeySQL(table, foreignKey.Name)
				if foreignKeyErr != nil {
					return Snapshot{}, foreignKeyErr
				}
				foreignKeySnapshots = append(foreignKeySnapshots, ForeignKeySnapshot{
					Name:            foreignKey.Name,
					ReferencedTable: foreignKey.ReferencedTable.Name,
					SQL:             foreignKeySQL,
				})
			}
			slices.SortFunc(foreignKeySnapshots, func(a, b ForeignKeySnapshot) int {
				return compareStrings(a.Name, b.Name)
			})
		}

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
			IsView:         tableDef.IsView,
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

func orderManagedTables(tables []schema.TableReference) ([]schema.TableReference, error) {
	cloned := slices.Clone(tables)
	tableByName := make(map[string]schema.TableReference, len(cloned))
	inDegree := make(map[string]int, len(cloned))
	dependents := make(map[string][]string, len(cloned))

	for _, table := range cloned {
		if table == nil || table.TableDef() == nil {
			return nil, fmt.Errorf("migrator: managed tables must be non-nil")
		}
		name := table.TableDef().Name
		if _, exists := tableByName[name]; exists {
			return nil, fmt.Errorf("migrator: duplicate table %q in registry", name)
		}
		tableByName[name] = table
		inDegree[name] = 0
	}

	for _, table := range cloned {
		tableDef := table.TableDef()
		seenDeps := make(map[string]struct{})
		for _, foreignKey := range tableDef.ForeignKeys {
			addTableDependency(tableByName, inDegree, dependents, seenDeps, tableDef.Name, foreignKey.ReferencedTable)
		}
		for _, constraint := range tableDef.Constraints {
			if constraint.Type != schema.ConstraintForeignKey {
				continue
			}
			addTableDependency(tableByName, inDegree, dependents, seenDeps, tableDef.Name, constraint.ReferencedTable)
		}
	}

	ready := make([]string, 0, len(cloned))
	for name, degree := range inDegree {
		if degree == 0 {
			ready = append(ready, name)
		}
	}
	slices.Sort(ready)

	ordered := make([]schema.TableReference, 0, len(cloned))
	for len(ready) != 0 {
		name := ready[0]
		ready = ready[1:]
		ordered = append(ordered, tableByName[name])

		children := append([]string(nil), dependents[name]...)
		slices.Sort(children)
		for _, child := range children {
			inDegree[child]--
			if inDegree[child] == 0 {
				ready = append(ready, child)
				slices.Sort(ready)
			}
		}
	}

	if len(ordered) != len(cloned) {
		return nil, fmt.Errorf("migrator: managed tables contain a circular foreign-key dependency")
	}

	return ordered, nil
}

func addTableDependency(
	tableByName map[string]schema.TableReference,
	inDegree map[string]int,
	dependents map[string][]string,
	seenDeps map[string]struct{},
	tableName string,
	referencedTable *schema.TableDef,
) {
	if referencedTable == nil {
		return
	}
	dependencyName := referencedTable.Name
	if dependencyName == tableName {
		return
	}
	if _, managed := tableByName[dependencyName]; !managed {
		return
	}
	if _, exists := seenDeps[dependencyName]; exists {
		return
	}
	seenDeps[dependencyName] = struct{}{}
	inDegree[tableName]++
	dependents[dependencyName] = append(dependents[dependencyName], tableName)
}
