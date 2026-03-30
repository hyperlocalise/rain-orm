// Package migrator provides schema snapshot, diff, and SQL migration helpers for the Rain CLI.
package migrator

import "github.com/hyperlocalise/rain-orm/pkg/schema"

// Registry returns the managed tables that participate in migration generation.
type Registry func() []schema.TableReference
