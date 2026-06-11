package rain_test

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestUpdateOrderLimitToSQL(t *testing.T) {
	users, _ := defineTables()

	tests := []struct {
		name    string
		dialect string
		setup   func(q *rain.UpdateQuery)
		wantSQL string
		wantErr string
	}{
		{
			name:    "sqlite order and limit",
			dialect: "sqlite",
			setup: func(q *rain.UpdateQuery) {
				q.Set(users.Name, "Alice").
					Where(users.Active.Eq(true)).
					OrderBy(users.ID.Asc()).
					Limit(10)
			},
			wantSQL: `UPDATE "users" SET "name" = ? WHERE "users"."active" = ? ORDER BY "users"."id" ASC LIMIT 10`,
		},
		{
			name:    "mysql order and limit",
			dialect: "mysql",
			setup: func(q *rain.UpdateQuery) {
				q.Set(users.Name, "Alice").
					Where(users.Active.Eq(true)).
					OrderBy(users.ID.Asc()).
					Limit(10)
			},
			wantSQL: "UPDATE `users` SET `name` = ? WHERE `users`.`active` = ? ORDER BY `users`.`id` ASC LIMIT 10",
		},
		{
			name:    "postgres order error",
			dialect: "postgres",
			setup: func(q *rain.UpdateQuery) {
				q.Set(users.Name, "Alice").
					Where(users.Active.Eq(true)).
					OrderBy(users.ID.Asc())
			},
			wantErr: "rain: ORDER BY is not supported for this query type in postgres dialect",
		},
		{
			name:    "postgres limit error",
			dialect: "postgres",
			setup: func(q *rain.UpdateQuery) {
				q.Set(users.Name, "Alice").
					Where(users.Active.Eq(true)).
					Limit(10)
			},
			wantErr: "rain: LIMIT/OFFSET is not supported for this query type in postgres dialect",
		},
		{
			name:    "sqlite with cte",
			dialect: "sqlite",
			setup: func(q *rain.UpdateQuery) {
				db, _ := rain.OpenDialect("sqlite")
				sub := db.Select().
					Table(users).
					Column(users.ID).
					Where(users.Active.Eq(false))

				q.With("inactive_users", sub).
					Set(users.Active, true).
					Where(users.ID.InSubquery(schema.Raw(`SELECT id FROM inactive_users`)))
			},
			wantSQL: `WITH "inactive_users" AS (SELECT "users"."id" FROM "users" WHERE "users"."active" = ?) UPDATE "users" SET "active" = ? WHERE "users"."id" IN (SELECT id FROM inactive_users)`,
		},
		{
			name:    "mysql with cte",
			dialect: "mysql",
			setup: func(q *rain.UpdateQuery) {
				db, _ := rain.OpenDialect("mysql")
				sub := db.Select().
					Table(users).
					Column(users.ID).
					Where(users.Active.Eq(false))

				q.With("inactive_users", sub).
					Set(users.Active, true).
					Where(users.ID.InSubquery(schema.Raw(`SELECT id FROM inactive_users`)))
			},
			wantSQL: "WITH `inactive_users` AS (SELECT `users`.`id` FROM `users` WHERE `users`.`active` = ?) UPDATE `users` SET `active` = ? WHERE `users`.`id` IN (SELECT id FROM inactive_users)",
		},
		{
			name:    "postgres update with alias",
			dialect: "postgres",
			setup: func(q *rain.UpdateQuery) {
				u := schema.Alias(users, "u")
				q.Table(u).
					Set(u.Name, "New Name").
					Where(u.ID.Eq(int64(1)))
			},
			wantSQL: `UPDATE "users" AS "u" SET "name" = $1 WHERE "u"."id" = $2`,
		},
		{
			name:    "postgres update from",
			dialect: "postgres",
			setup: func(q *rain.UpdateQuery) {
				_, posts := defineTables()
				u := schema.Alias(users, "u")
				p := schema.Alias(posts, "p")
				q.Table(u).
					From(p).
					Set(u.Name, "Alice").
					Where(u.ID.EqCol(p.UserID)).
					Where(p.ID.Eq(int64(10)))
			},
			wantSQL: `UPDATE "users" AS "u" SET "name" = $1 FROM "posts" AS "p" WHERE ("u"."id" = "p"."user_id" AND "p"."id" = $2)`,
		},
		{
			name:    "sqlite update from",
			dialect: "sqlite",
			setup: func(q *rain.UpdateQuery) {
				_, posts := defineTables()
				u := schema.Alias(users, "u")
				p := schema.Alias(posts, "p")
				q.Table(u).
					From(p).
					Set(u.Name, "Alice").
					Where(u.ID.EqCol(p.UserID)).
					Where(p.ID.Eq(int64(10)))
			},
			wantSQL: `UPDATE "users" AS "u" SET "name" = ? FROM "posts" AS "p" WHERE ("u"."id" = "p"."user_id" AND "p"."id" = ?)`,
		},
		{
			name:    "postgres update from subquery",
			dialect: "postgres",
			setup: func(q *rain.UpdateQuery) {
				db, _ := rain.OpenDialect("postgres")
				_, posts := defineTables()
				u := schema.Alias(users, "u")
				sub := db.Select().
					Table(posts).
					Column(posts.UserID, schema.Count().As("count")).
					GroupBy(posts.UserID)

				q.Table(u).
					FromSubquery(sub, "stats").
					Set(u.Active, false).
					Where(u.ID.Eq(int64(1))).
					Where(schema.Raw(`stats.count > 10`))
			},
			wantSQL: `UPDATE "users" AS "u" SET "active" = $1 FROM (SELECT "posts"."user_id", COUNT(*) AS "count" FROM "posts" GROUP BY "posts"."user_id") AS "stats" WHERE ("u"."id" = $2 AND stats.count > 10)`,
		},
		{
			name:    "mysql update from error",
			dialect: "mysql",
			setup: func(q *rain.UpdateQuery) {
				_, posts := defineTables()
				q.From(posts).
					Set(users.Name, "Alice").
					Where(users.ID.Eq(int64(1)))
			},
			wantErr: "rain: UPDATE ... FROM is not supported by mysql dialect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatal(err)
			}

			q := db.Update().Table(users)
			tt.setup(q)

			gotSQL, _, err := q.ToSQL()
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("ToSQL() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("ToSQL() unexpected error: %v", err)
				return
			}

			if gotSQL != tt.wantSQL {
				t.Errorf("ToSQL() gotSQL = %q, want %q", gotSQL, tt.wantSQL)
			}
		})
	}
}
