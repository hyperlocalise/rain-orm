package rain

import (
	"testing"

	"github.com/hyperlocalise/rain-orm/pkg/schema"
)

func TestCreateTableSQLSerialTypesReal(t *testing.T) {
	type SmallSerialTable struct {
		schema.TableModel
		ID *schema.Column[int16]
	}
	type SerialTable struct {
		schema.TableModel
		ID *schema.Column[int32]
	}
	type BigSerialTable struct {
		schema.TableModel
		ID *schema.Column[int64]
	}

	dbPg := MustOpenDialect("postgres")
	dbSqlite := MustOpenDialect("sqlite")
	dbMysql := MustOpenDialect("mysql")

	t.Run("PostgresSmallSerial", func(t *testing.T) {
		tbl := schema.Define("test", func(t *SmallSerialTable) {
			t.ID = t.SmallSerial("id").PrimaryKey()
		})
		got, _ := dbPg.CreateTableSQL(tbl)
		want := "CREATE TABLE \"test\" (\n\t\"id\" SMALLSERIAL PRIMARY KEY\n)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("PostgresSerial", func(t *testing.T) {
		tbl := schema.Define("test", func(t *SerialTable) {
			t.ID = t.Serial("id").PrimaryKey()
		})
		got, _ := dbPg.CreateTableSQL(tbl)
		want := "CREATE TABLE \"test\" (\n\t\"id\" SERIAL PRIMARY KEY\n)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("PostgresBigSerial", func(t *testing.T) {
		tbl := schema.Define("test", func(t *BigSerialTable) {
			t.ID = t.BigSerial("id").PrimaryKey()
		})
		got, _ := dbPg.CreateTableSQL(tbl)
		want := "CREATE TABLE \"test\" (\n\t\"id\" BIGSERIAL PRIMARY KEY\n)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("SQLiteSmallSerial", func(t *testing.T) {
		tbl := schema.Define("test", func(t *SmallSerialTable) {
			t.ID = t.SmallSerial("id").PrimaryKey()
		})
		got, _ := dbSqlite.CreateTableSQL(tbl)
		want := "CREATE TABLE \"test\" (\n\t\"id\" INTEGER PRIMARY KEY AUTOINCREMENT\n)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("SQLiteSerial", func(t *testing.T) {
		tbl := schema.Define("test", func(t *SerialTable) {
			t.ID = t.Serial("id").PrimaryKey()
		})
		got, _ := dbSqlite.CreateTableSQL(tbl)
		want := "CREATE TABLE \"test\" (\n\t\"id\" INTEGER PRIMARY KEY AUTOINCREMENT\n)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("MySQLSmallSerial", func(t *testing.T) {
		tbl := schema.Define("test", func(t *SmallSerialTable) {
			t.ID = t.SmallSerial("id").PrimaryKey()
		})
		got, _ := dbMysql.CreateTableSQL(tbl)
		want := "CREATE TABLE `test` (\n\t`id` SMALLINT PRIMARY KEY AUTO_INCREMENT\n)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("MySQLSerial", func(t *testing.T) {
		tbl := schema.Define("test", func(t *SerialTable) {
			t.ID = t.Serial("id").PrimaryKey()
		})
		got, _ := dbMysql.CreateTableSQL(tbl)
		want := "CREATE TABLE `test` (\n\t`id` INT PRIMARY KEY AUTO_INCREMENT\n)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
