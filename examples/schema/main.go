package main

import (
	"fmt"

	"github.com/hyperlocalise/rain-orm/examples/schema/registry"
)

func main() {
	fmt.Printf("table=%s columns=%d indexes=%d constraints=%d fks=%d\n",
		registry.Users.TableDef().Name,
		len(registry.Users.TableDef().Columns),
		len(registry.Users.TableDef().Indexes),
		len(registry.Users.TableDef().Constraints),
		len(registry.Users.TableDef().ForeignKeys),
	)
	fmt.Printf("posts fk: %s -> %s.%s\n",
		registry.Posts.TableDef().ForeignKeys[0].Column.Name,
		registry.Posts.TableDef().ForeignKeys[0].ReferencedTable.Name,
		registry.Posts.TableDef().ForeignKeys[0].ReferencedColumn.Name,
	)
	fmt.Printf("memberships constraints=%d indexes=%d\n",
		len(registry.Memberships.TableDef().Constraints),
		len(registry.Memberships.TableDef().Indexes),
	)
}
