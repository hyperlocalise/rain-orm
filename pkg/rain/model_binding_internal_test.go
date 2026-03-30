package rain

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

type bindingProfile struct {
	Theme string `json:"theme"`
	Admin bool   `json:"admin"`
}

type bindingUsersTable struct {
	schema.TableModel
	ID        *schema.Column[int64]
	Email     *schema.Column[string]
	Active    *schema.Column[bool]
	CreatedAt *schema.Column[time.Time]
	Profile   *schema.Column[any]
}

func defineBindingUsersTable() *bindingUsersTable {
	return schema.Define("binding_users", func(t *bindingUsersTable) {
		t.ID = t.BigSerial("id").PrimaryKey()
		t.Email = t.VarChar("email", 255).NotNull()
		t.Active = t.Boolean("active").NotNull().Default(true)
		t.CreatedAt = t.TimestampTZ("created_at").NotNull().DefaultNow()
		t.Profile = t.JSONB("profile").NotNull()
	})
}

func TestBindTableModelValidatesInferredMappings(t *testing.T) {
	t.Parallel()

	users := defineBindingUsersTable()
	type inferredUser struct {
		ID        int64
		Email     string
		Active    bool
		CreatedAt time.Time
		Profile   JSON[bindingProfile]
	}

	if err := BindTableModel[inferredUser](users); err != nil {
		t.Fatalf("BindTableModel returned error: %v", err)
	}
}

func TestBindTableModelRejectsUnknownColumnsAndDuplicateMappings(t *testing.T) {
	t.Parallel()

	users := defineBindingUsersTable()

	type unknownColumn struct {
		Email string `db:"email"`
		Ghost string `db:"ghost"`
	}
	if err := BindTableModel[unknownColumn](users); err == nil || !strings.Contains(err.Error(), `unknown column "ghost"`) {
		t.Fatalf("expected unknown column error, got %v", err)
	}

	type duplicateColumn struct {
		Email    string `db:"email"`
		EmailDup string `db:"email"`
	}
	if err := BindTableModel[duplicateColumn](users); err == nil || !strings.Contains(err.Error(), `duplicate model field mapping`) {
		t.Fatalf("expected duplicate mapping error, got %v", err)
	}
}

func TestBindTableModelRejectsIncompatibleTypes(t *testing.T) {
	t.Parallel()

	users := defineBindingUsersTable()
	type invalidUser struct {
		Email  string
		Active string
	}

	if err := BindTableModel[invalidUser](users); err == nil || !strings.Contains(err.Error(), `not compatible`) {
		t.Fatalf("expected incompatible type error, got %v", err)
	}
}

func TestBindTableModelCacheUsesTableIdentity(t *testing.T) {
	t.Parallel()

	type sharedOne struct {
		schema.TableModel
		Email *schema.Column[string]
	}
	type sharedTwo struct {
		schema.TableModel
		Active *schema.Column[bool]
	}

	first := schema.Define("shared_binding", func(t *sharedOne) {
		t.Email = t.VarChar("email", 255).NotNull()
	})
	second := schema.Define("shared_binding", func(t *sharedTwo) {
		t.Active = t.Boolean("active").NotNull()
	})

	type emailModel struct {
		Email string
	}

	if err := BindTableModel[emailModel](first); err != nil {
		t.Fatalf("bind first table: %v", err)
	}
	if err := BindTableModel[emailModel](second); err == nil || !strings.Contains(err.Error(), `unknown column "email"`) {
		t.Fatalf("expected second same-name table binding to fail, got %v", err)
	}
	if _, err := lookupModelAssignmentPlan(second.TableDef(), reflect.TypeFor[emailModel]()); err == nil || !strings.Contains(err.Error(), `unknown column "email"`) {
		t.Fatalf("expected assignment plan lookup to fail for second same-name table, got %v", err)
	}
}

func TestJSONWrapperRoundTripAndLazyInference(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openInternalQueryDB(t)
	users := defineBindingUsersTable()

	statement, err := db.CreateTableSQL(users)
	if err != nil {
		t.Fatalf("CreateTableSQL failed: %v", err)
	}
	if _, err := db.Exec(ctx, statement); err != nil {
		t.Fatalf("create table: %v", err)
	}

	type userRow struct {
		ID      int64
		Email   string
		Active  bool
		Profile JSON[bindingProfile]
	}

	inserted := userRow{
		Email:  "json@example.com",
		Active: true,
		Profile: JSON[bindingProfile]{
			V: bindingProfile{Theme: "night", Admin: true},
		},
	}
	if _, err := db.Insert().Table(users).Model(&inserted).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var scanned userRow
	if err := db.Select().Table(users).Where(users.Email.Eq(inserted.Email)).Scan(ctx, &scanned); err != nil {
		t.Fatalf("scan user: %v", err)
	}
	if scanned.Email != inserted.Email || !scanned.Active {
		t.Fatalf("unexpected scanned row: %#v", scanned)
	}
	if scanned.Profile.V.Theme != "night" || !scanned.Profile.V.Admin {
		t.Fatalf("unexpected scanned JSON payload: %#v", scanned.Profile)
	}

	rawValue, err := scanned.Profile.Value()
	if err != nil {
		t.Fatalf("JSON Value returned error: %v", err)
	}
	encoded, ok := rawValue.([]byte)
	if !ok {
		t.Fatalf("expected JSON Value to return []byte, got %T", rawValue)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal encoded JSON: %v", err)
	}
	if payload["theme"] != "night" {
		t.Fatalf("unexpected encoded JSON payload: %#v", payload)
	}
}
