package platform

import (
	"context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnowflakeClockRollbackAndSequenceOverflow(t *testing.T) {
	now := snowflakeEpoch + 10000
	first, last, seq, e := snowflakeNext(0, 0, 13, now)
	if e != nil || seq != 0 {
		t.Fatal(first, last, seq, e)
	}
	next, last, seq, e := snowflakeNext(last, seq, 13, now-100)
	if e != nil || next <= first {
		t.Fatal("rollback repeated a number", next, e)
	}
	value, last, seq, e := snowflakeNext(last, 4095, 13, now)
	if e != nil || seq != 0 || last != 10001 || value <= next {
		t.Fatal("sequence rollover", value, last, seq, e)
	}
	if _, _, _, e = snowflakeNext(last, seq, 13, now-6000); status.Code(e) != codes.Unavailable {
		t.Fatalf("large clock regression must fail closed: %v", e)
	}
}
func TestBusinessIDsPersistAcrossRestartAndDeduplicateConcurrentRequests(t *testing.T) {
	t.Setenv("SNOWFLAKE_NODE_ID", "")
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ids.db")
	db, e := OpenDB(ctx, "sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if e = MigrateBusinessIDs(ctx, db); e != nil {
		t.Fatal(e)
	}
	seen := sync.Map{}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Go(func() {
			id, e := NextBusinessID(ctx, db, "order")
			if e != nil {
				t.Error(e)
				return
			}
			if !IsBusinessID(id, "ORD") {
				t.Error(id)
			}
			if _, loaded := seen.LoadOrStore(id, true); loaded {
				t.Error("duplicate", id)
			}
		})
	}
	wg.Wait()
	var previous string
	for i := 0; i < 32; i++ {
		wg.Go(func() {
			id, e := ResolveBusinessID(ctx, db, "refund", "same-request", "", "")
			if e != nil {
				t.Error(e)
				return
			}
			if old, loaded := seen.LoadOrStore("dedup", id); loaded && old != id {
				t.Error("different numbers for one request", old, id)
			}
		})
	}
	wg.Wait()
	previousValue, _ := seen.Load("dedup")
	previous = previousValue.(string)
	sql, _ := db.DB()
	sql.Close()
	db, e = OpenDB(ctx, "sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	sql, _ = db.DB()
	defer sql.Close()
	current, e := ResolveBusinessID(ctx, db, "refund", "same-request", "", "")
	if e != nil || current != previous {
		t.Fatal("lost persisted idempotency mapping", current, previous, e)
	}
	next, e := NextBusinessID(ctx, db, "refund")
	if e != nil || next == previous {
		t.Fatal("restart repeated number", next, e)
	}
}
func TestBusinessIDsRetainHistoricalReferencesAndRejectBadNodes(t *testing.T) {
	ctx := context.Background()
	db, e := OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if e != nil {
		t.Fatal(e)
	}
	sql, _ := db.DB()
	defer sql.Close()
	if e = MigrateBusinessIDs(ctx, db); e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("CREATE TABLE historical_orders (id TEXT PRIMARY KEY)").Error; e != nil {
		t.Fatal(e)
	}
	if e = db.Exec("INSERT INTO historical_orders (id) VALUES (?)", "ord_legacy").Error; e != nil {
		t.Fatal(e)
	}
	id, e := ResolveBusinessID(ctx, db, "order", "old-key", "ord_legacy", "historical_orders")
	if e != nil || id != "ord_legacy" {
		t.Fatal(id, e)
	}
	t.Setenv("SNOWFLAKE_NODE_ID", "1024")
	if _, e = NextBusinessID(ctx, db, "order"); status.Code(e) != codes.FailedPrecondition {
		t.Fatal(e)
	}
	t.Setenv("SNOWFLAKE_NODE_ID", "27")
	id, e = NextBusinessID(ctx, db, "payment")
	if e != nil || !strings.HasPrefix(id, "PAY_") {
		t.Fatal(id, e)
	}
	cancelled, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	if _, e = NextBusinessID(cancelled, db, "order"); e == nil {
		t.Fatal("ignored context cancellation")
	}
}
