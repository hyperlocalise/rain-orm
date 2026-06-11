package rain_test

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/rain"
	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestDeleteOrderLimitToSQL(t *testing.T) {
	users, _ := defineTables()

	tests := []struct {
		name    string
		dialect string
		setup   func(q *rain.DeleteQuery)
		wantSQL string
		wantErr string
	}{
		{
			name:    "sqlite order and limit",
			dialect: "sqlite",
			setup: func(q *rain.DeleteQuery) {
				q.Where(users.Active.Eq(false)).
					OrderBy(users.ID.Asc()).
					Limit(10)
			},
			wantSQL: `DELETE FROM "users" WHERE "users"."active" = ? ORDER BY "users"."id" ASC LIMIT 10`,
		},
		{
			name:    "mysql order and limit",
			dialect: "mysql",
			setup: func(q *rain.DeleteQuery) {
				q.Where(users.Active.Eq(false)).
					OrderBy(users.ID.Asc()).
					Limit(10)
			},
			wantSQL: "DELETE FROM `users` WHERE `users`.`active` = ? ORDER BY `users`.`id` ASC LIMIT 10",
		},
		{
			name:    "postgres order error",
			dialect: "postgres",
			setup: func(q *rain.DeleteQuery) {
				q.Where(users.Active.Eq(false)).
					OrderBy(users.ID.Asc())
			},
			wantErr: "rain: ORDER BY is not supported for this query type in postgres dialect",
		},
		{
			name:    "postgres limit error",
			dialect: "postgres",
			setup: func(q *rain.DeleteQuery) {
				q.Where(users.Active.Eq(false)).
					Limit(10)
			},
			wantErr: "rain: LIMIT/OFFSET is not supported for this query type in postgres dialect",
		},
		{
			name:    "sqlite with cte",
			dialect: "sqlite",
			setup: func(q *rain.DeleteQuery) {
				db, _ := rain.OpenDialect("sqlite")
				sub := db.Select().
					Table(users).
					Column(users.ID).
					Where(users.Active.Eq(false))

				q.With("inactive_users", sub).
					Where(users.ID.InSubquery(schema.Raw(`SELECT id FROM inactive_users`)))
			},
			wantSQL: `WITH "inactive_users" AS (SELECT "users"."id" FROM "users" WHERE "users"."active" = ?) DELETE FROM "users" WHERE "users"."id" IN (SELECT id FROM inactive_users)`,
		},
		{
			name:    "mysql with cte",
			dialect: "mysql",
			setup: func(q *rain.DeleteQuery) {
				db, _ := rain.OpenDialect("mysql")
				sub := db.Select().
					Table(users).
					Column(users.ID).
					Where(users.Active.Eq(false))

				q.With("inactive_users", sub).
					Where(users.ID.InSubquery(schema.Raw(`SELECT id FROM inactive_users`)))
			},
			wantSQL: "WITH `inactive_users` AS (SELECT `users`.`id` FROM `users` WHERE `users`.`active` = ?) DELETE FROM `users` WHERE `users`.`id` IN (SELECT id FROM inactive_users)",
		},
		{
			name:    "postgres delete with alias",
			dialect: "postgres",
			setup: func(q *rain.DeleteQuery) {
				u := schema.Alias(users, "u")
				q.Table(u).Where(u.ID.Eq(int64(1)))
			},
			wantSQL: `DELETE FROM "users" AS "u" WHERE "u"."id" = $1`,
		},
		{
			name:    "postgres delete using",
			dialect: "postgres",
			setup: func(q *rain.DeleteQuery) {
				_, posts := defineTables()
				u := schema.Alias(users, "u")
				p := schema.Alias(posts, "p")
				q.Table(u).
					Using(p).
					Where(u.ID.EqCol(p.UserID)).
					Where(p.ID.Eq(int64(10)))
			},
			wantSQL: `DELETE FROM "users" AS "u" USING "posts" AS "p" WHERE ("u"."id" = "p"."user_id" AND "p"."id" = $1)`,
		},
		{
			name:    "sqlite delete using error",
			dialect: "sqlite",
			setup: func(q *rain.DeleteQuery) {
				_, posts := defineTables()
				q.Using(posts).Where(users.ID.Eq(int64(1)))
			},
			wantErr: "rain: DELETE ... USING is not supported by sqlite dialect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := rain.OpenDialect(tt.dialect)
			if err != nil {
				t.Fatal(err)
			}

			q := db.Delete().Table(users)
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
