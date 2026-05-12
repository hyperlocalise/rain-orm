package rain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestSelectLockingToSQL(t *testing.T) {
	t.Parallel()

	users, posts := defineTables()

	type tc struct {
		name    string
		dialect string
		build   func(*rain.DB) *rain.SelectQuery
		wantSQL string
		wantErr string
	}

	cases := []tc{
		{
			name:    "for update postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate()
			},
			wantSQL: `SELECT * FROM "users" FOR UPDATE`,
		},
		{
			name:    "for share mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForShare()
			},
			wantSQL: "SELECT * FROM `users` FOR SHARE",
		},
		{
			name:    "for update nowait postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate(rain.LockConfig{NoWait: true})
			},
			wantSQL: `SELECT * FROM "users" FOR UPDATE NOWAIT`,
		},
		{
			name:    "for update skip locked mysql",
			dialect: "mysql",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate(rain.LockConfig{SkipLocked: true})
			},
			wantSQL: "SELECT * FROM `users` FOR UPDATE SKIP LOCKED",
		},
		{
			name:    "for update of tables postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).Join(posts, posts.UserID.EqCol(users.ID)).ForUpdate(rain.LockConfig{Of: []schema.TableReference{users, posts}})
			},
			wantSQL: `SELECT * FROM "users" INNER JOIN "posts" ON "posts"."user_id" = "users"."id" FOR UPDATE OF "users", "posts"`,
		},
		{
			name:    "for update of aliased tables postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				u := schema.Alias(users, "u")
				p := schema.Alias(posts, "p")
				return db.Select().Table(u).Join(p, p.UserID.EqCol(u.ID)).ForUpdate(rain.LockConfig{Of: []schema.TableReference{u, p}})
			},
			wantSQL: `SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" FOR UPDATE OF "u", "p"`,
		},
		{
			name:    "for no key update postgres",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).For(rain.LockNoKeyUpdate)
			},
			wantSQL: `SELECT * FROM "users" FOR NO KEY UPDATE`,
		},
		{
			name:    "locking unsupported on sqlite",
			dialect: "sqlite",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate()
			},
			wantErr: "select locking is not supported by sqlite dialect",
		},
		{
			name:    "locking with compound query error",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				q1 := db.Select().Table(users)
				q2 := db.Select().Table(users)
				return q1.Union(q2).ForUpdate()
			},
			wantErr: "compound queries do not support FOR locking clauses",
		},
		{
			name:    "locking with count error",
			dialect: "postgres",
			build: func(db *rain.DB) *rain.SelectQuery {
				return db.Select().Table(users).ForUpdate()
			},
			wantErr: "aggregate helpers do not support FOR locking clauses",
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatalf("OpenDialect returned error: %v", err)
			}

			q := tt.build(db)

			if tt.name == "locking with count error" {
				_, err := q.Count(context.Background())
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			sqlText, _, err := q.ToSQL()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ToSQL returned error: %v", err)
			}
			if sqlText != tt.wantSQL {
				t.Fatalf("unexpected SQL:\nwant: %s\ngot:  %s", tt.wantSQL, sqlText)
			}
		})
	}
}
