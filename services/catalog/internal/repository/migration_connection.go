package repository

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"time"
)

// Full-text indexed catalog tables can require an online table rebuild even
// when only a version column is added. The normal request DSN's short socket
// read timeout must not disconnect such DDL halfway through. Only migration
// uses this separate pool; request deadlines and the runtime pool are unchanged.
func migrationDSN(dsn string) (string, error) {
	cfg, e := mysqldriver.ParseDSN(dsn)
	if e != nil {
		return "", fmt.Errorf("invalid catalog migration connection configuration")
	}
	cfg.ReadTimeout = 5 * time.Minute
	return cfg.FormatDSN(), nil
}
func (r *Repository) migrateWithConnection(ctx context.Context) error {
	dialect, ok := r.db.Dialector.(*gormmysql.Dialector)
	if !ok || dialect.DSN == "" {
		return r.migrate(ctx)
	}
	dsn, e := migrationDSN(dialect.DSN)
	if e != nil {
		return e
	}
	db, e := platform.OpenDB(ctx, "mysql", dsn)
	if e != nil {
		return fmt.Errorf("open catalog migration connection: %w", e)
	}
	pool, e := db.DB()
	if e != nil {
		return e
	}
	defer pool.Close()
	pool.SetMaxOpenConns(2)
	pool.SetMaxIdleConns(1)
	copy := *r
	copy.db = db
	if e = copy.migrate(ctx); e != nil {
		return e
	}
	r.searchCategoryIndexed = copy.searchCategoryIndexed
	r.searchDocumentIndexed = copy.searchDocumentIndexed
	return nil
}
