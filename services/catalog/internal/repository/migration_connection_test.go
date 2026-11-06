package repository

import (
	mysqldriver "github.com/go-sql-driver/mysql"
	"strings"
	"testing"
	"time"
)

func TestCatalogMigrationConnectionKeepsRuntimeDSNAndSecurityOptions(t *testing.T) {
	original := "business:example-secret@tcp(127.0.0.1:3306)/catalog?parseTime=true&loc=UTC&timeout=3s&readTimeout=10s&writeTimeout=10s&tls=preferred&charset=utf8mb4"
	migrated, e := migrationDSN(original)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := mysqldriver.ParseDSN(original)
	after, e := mysqldriver.ParseDSN(migrated)
	if e != nil {
		t.Fatal(e)
	}
	if before.ReadTimeout != 10*time.Second || after.ReadTimeout != 5*time.Minute {
		t.Fatal("migration timeout not isolated")
	}
	after.ReadTimeout = before.ReadTimeout
	if after.FormatDSN() != before.FormatDSN() {
		t.Fatal("migration altered connection, security or write settings")
	}
	_, e = migrationDSN("example-secret@broken")
	if e == nil || strings.Contains(e.Error(), "example-secret") {
		t.Fatal("invalid DSN was not rejected safely")
	}
}
