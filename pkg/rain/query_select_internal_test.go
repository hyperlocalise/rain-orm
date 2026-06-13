package rain

import (
	"reflect"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestSelectRelationConfigInternal(t *testing.T) {
	t.Parallel()

	db, _ := OpenDialect("postgres")

	// Define simple tables for testing
	type usersTable struct {
		schema.TableModel
		ID *schema.Column[int64]
	}
	type postsTable struct {
		schema.TableModel
		ID     *schema.Column[int64]
		UserID *schema.Column[int64]
	}

	users := schema.Define("users", func(t *usersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
	})
	posts := schema.Define("posts", func(t *postsTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.UserID = t.BigInt("user_id").NotNull().References(users.ID)
	})
	users.HasMany("posts", users.ID, posts.UserID)

	cfg := RelationConfig{
		Where:   posts.ID.Gt(100),
		OrderBy: []schema.OrderExpr{posts.ID.Desc()},
	}

	q := db.Select().
		Table(users).
		Relation("posts", cfg)

	if len(q.relationConfigs) != 1 {
		t.Fatalf("expected 1 relation config, got %d", len(q.relationConfigs))
	}

	gotCfg, ok := q.relationConfigs["posts"]
	if !ok {
		t.Fatal("expected config for 'posts'")
	}
	if !reflect.DeepEqual(gotCfg, cfg) {
		t.Fatalf("config mismatch:\nwant: %#v\ngot:  %#v", cfg, gotCfg)
	}

	// Test cloning
	cloned := q.clone()
	if len(cloned.relationConfigs) != 1 {
		t.Fatalf("expected cloned to have 1 relation config, got %d", len(cloned.relationConfigs))
	}
	if !reflect.DeepEqual(cloned.relationConfigs["posts"], cfg) {
		t.Fatal("cloned config mismatch")
	}

	// Verify deep copy (modifying cloned doesn't affect original)
	cloned.relationConfigs["posts"] = RelationConfig{}
	if reflect.DeepEqual(q.relationConfigs["posts"], cloned.relationConfigs["posts"]) {
		t.Fatal("expected relationConfigs to be deep copied during clone")
	}
}
