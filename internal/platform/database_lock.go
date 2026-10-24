package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// WithServiceStartupLock serializes schema migration and optional seeding among
// replicas of the same service. MySQL DDL commits implicitly, so a transaction
// cannot provide this guard. The named lock stays on one dedicated connection.
func WithServiceStartupLock(ctx context.Context, db *gorm.DB, service string, initialize func(context.Context) error) error {
	_, err := withServiceDatabaseLock(ctx, db, service, "startup", 60, initialize)
	return err
}

// runServiceTick skips a replica's scan while another replica is running the
// same service's scheduled work. Business row locks and idempotency still apply
// to work initiated through RPC or messages.
func runServiceTick(ctx context.Context, db *gorm.DB, service string, tick func(context.Context) error) error {
	_, err := withServiceDatabaseLock(ctx, db, service, "tick", 0, tick)
	return err
}

func databaseLockName(namespace, service, purpose string) string {
	sum := sha256.Sum256([]byte(namespace + "\x00" + service + "\x00" + purpose))
	return "lumo:" + purpose + ":" + hex.EncodeToString(sum[:20])
}

func withServiceDatabaseLock(ctx context.Context, db *gorm.DB, service, purpose string, waitSeconds int, work func(context.Context) error) (bool, error) {
	// SQLite is only the isolated local/test runtime; deployed replicas use MySQL.
	if db.Dialector.Name() != "mysql" {
		return true, work(ctx)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return false, err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("reserve %s lock connection: %w", purpose, err)
	}
	defer conn.Close()
	var namespace sql.NullString
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&namespace); err != nil {
		return false, fmt.Errorf("resolve %s lock database: %w", purpose, err)
	}
	if !namespace.Valid {
		return false, fmt.Errorf("resolve %s lock database", purpose)
	}
	name := databaseLockName(namespace.String, service, purpose)
	acquired, err := acquireDatabaseLock(ctx, conn, name, waitSeconds)
	if err != nil {
		// A cancellation can race with lock acquisition on the server. Never put
		// that session back into the pool without releasing or discarding it.
		discardLockConnection(conn)
		return false, fmt.Errorf("acquire %s lock: %w", purpose, err)
	}
	if !acquired {
		if waitSeconds == 0 {
			return false, nil
		}
		return false, fmt.Errorf("acquire %s lock timed out", purpose)
	}
	defer func() {
		// Shutdown cancellation must not prevent RELEASE_LOCK. A broken release
		// discards the physical session, which also releases MySQL named locks.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var released sql.NullInt64
		if releaseErr := conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", name).Scan(&released); releaseErr != nil || !released.Valid || released.Int64 != 1 {
			discardLockConnection(conn)
		}
	}()
	return true, work(ctx)
}

func acquireDatabaseLock(ctx context.Context, conn *sql.Conn, name string, waitSeconds int) (bool, error) {
	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	for {
		// Poll with a zero database wait so a competing migration cannot exceed
		// the DSN's shorter readTimeout or prevent prompt shutdown cancellation.
		var acquired sql.NullInt64
		if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", name, 0).Scan(&acquired); err != nil {
			return false, err
		}
		if !acquired.Valid {
			return false, fmt.Errorf("database lock unavailable")
		}
		if acquired.Int64 == 1 {
			return true, nil
		}
		if waitSeconds == 0 || !time.Now().Before(deadline) {
			return false, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

func discardLockConnection(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}
