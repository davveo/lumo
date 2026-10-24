package platform

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Exercise dedicated SQL sessions and pool reuse without requiring a broker or
// mutating the running MySQL database. The driver models MySQL named-lock scope.
type namedLockDriver struct {
	mu             sync.Mutex
	owner          map[string]int
	next           int
	releaseFailure bool
	acquireFailure bool
}

type namedLockConn struct {
	server    *namedLockDriver
	id        int
	namespace string
}

type namedLockRows struct {
	value driver.Value
	done  bool
}

func (d *namedLockDriver) Open(namespace string) (driver.Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	return &namedLockConn{server: d, id: d.next, namespace: namespace}, nil
}
func (c *namedLockConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *namedLockConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *namedLockConn) Close() error {
	c.server.mu.Lock()
	defer c.server.mu.Unlock()
	for name, owner := range c.server.owner {
		if owner == c.id {
			delete(c.server.owner, name)
		}
	}
	return nil
}
func (c *namedLockConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.server.mu.Lock()
	defer c.server.mu.Unlock()
	if query == "SELECT DATABASE()" {
		return &namedLockRows{value: c.namespace}, nil
	}
	name := args[0].Value.(string)
	var result int64
	switch query {
	case "SELECT GET_LOCK(?, ?)":
		if c.server.owner[name] == 0 || c.server.owner[name] == c.id {
			c.server.owner[name] = c.id
			result = 1
		}
		if c.server.acquireFailure {
			c.server.acquireFailure = false
			return nil, context.DeadlineExceeded
		}
	case "SELECT RELEASE_LOCK(?)":
		if c.server.releaseFailure {
			c.server.releaseFailure = false
			return nil, errors.New("release failed")
		}
		if c.server.owner[name] == c.id {
			delete(c.server.owner, name)
			result = 1
		}
	default:
		return nil, fmt.Errorf("unexpected SQL %q", query)
	}
	return &namedLockRows{value: result}, nil
}
func (*namedLockRows) Columns() []string { return []string{"result"} }
func (*namedLockRows) Close() error      { return nil }
func (r *namedLockRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	values[0], r.done = r.value, true
	return nil
}

var namedLockDriverSequence atomic.Uint64

func namedLockDB(t *testing.T, server *namedLockDriver, namespace string) *gorm.DB {
	t.Helper()
	name := fmt.Sprintf("lumo-lock-test-%d", namedLockDriverSequence.Add(1))
	sql.Register(name, server)
	raw, err := sql.Open(name, namespace)
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(5)
	t.Cleanup(func() { _ = raw.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: raw, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestServiceStartupLockSerializesReplicas(t *testing.T) {
	server := &namedLockDriver{owner: map[string]int{}}
	first, second := namedLockDB(t, server, "lumo_order"), namedLockDB(t, server, "lumo_order")
	entered, release := make(chan struct{}), make(chan struct{})
	results := make(chan error, 2)
	go func() {
		results <- WithServiceStartupLock(context.Background(), first, "order", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	secondEntered := make(chan struct{})
	go func() {
		results <- WithServiceStartupLock(context.Background(), second, "order", func(context.Context) error { close(secondEntered); return nil })
	}()
	select {
	case <-secondEntered:
		t.Fatal("second replica migrated while first held the startup lock")
	case <-time.After(120 * time.Millisecond):
	}
	close(release)
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("startup lock did not transfer")
		}
	}
}

func TestServiceTickLockSkipsReplicaAndSeparatesServicesAndSchemas(t *testing.T) {
	server := &namedLockDriver{owner: map[string]int{}}
	first, second := namedLockDB(t, server, "lumo_order"), namedLockDB(t, server, "lumo_order")
	otherSchema := namedLockDB(t, server, "lumo_other")
	workError := errors.New("tick failed")
	err := runServiceTick(context.Background(), first, "order", func(ctx context.Context) error {
		called := false
		if err := runServiceTick(ctx, second, "order", func(context.Context) error { called = true; return nil }); err != nil || called {
			t.Fatalf("competing replica ran: called=%v err=%v", called, err)
		}
		for _, test := range []struct {
			db      *gorm.DB
			service string
		}{{second, "checkout"}, {otherSchema, "order"}} {
			called = false
			if err := runServiceTick(ctx, test.db, test.service, func(context.Context) error { called = true; return nil }); err != nil || !called {
				t.Fatalf("independent service/schema blocked: called=%v err=%v", called, err)
			}
		}
		return workError
	})
	if !errors.Is(err, workError) {
		t.Fatal(err)
	}
	called := false
	if err := runServiceTick(context.Background(), second, "order", func(context.Context) error { called = true; return nil }); err != nil || !called {
		t.Fatalf("failed work did not release: called=%v err=%v", called, err)
	}
}

func TestServiceDatabaseLockCancellationAndBrokenSessionsReleaseOwnership(t *testing.T) {
	server := &namedLockDriver{owner: map[string]int{}}
	first, second := namedLockDB(t, server, "lumo_order"), namedLockDB(t, server, "lumo_order")
	ctx, cancel := context.WithCancel(context.Background())
	if err := WithServiceStartupLock(ctx, first, "order", func(context.Context) error { cancel(); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"release", "acquire"} {
		server.mu.Lock()
		server.releaseFailure, server.acquireFailure = failure == "release", failure == "acquire"
		server.mu.Unlock()
		err := runServiceTick(context.Background(), first, "order", func(context.Context) error { return nil })
		if failure == "acquire" && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected acquisition failure, got %v", err)
		}
		called := false
		if err := runServiceTick(context.Background(), second, "order", func(context.Context) error { called = true; return nil }); err != nil || !called {
			t.Fatalf("broken %s leaked named lock: called=%v err=%v", failure, called, err)
		}
	}
	if err := WithServiceStartupLock(context.Background(), first, "order", func(context.Context) error {
		cancelled, cancelWait := context.WithCancel(context.Background())
		cancelWait()
		return WithServiceStartupLock(cancelled, second, "order", func(context.Context) error { t.Fatal("cancelled startup ran"); return nil })
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter returned %v", err)
	}
}

func TestOutboxReplicaClaimFencesExpiredPublisher(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if err := Enqueue(ctx, db, Event{ID: "replica-event", Type: "OrderCreated"}); err != nil {
		t.Fatal(err)
	}
	first, owned, err := claimOutbox(ctx, db, "replica-event")
	if err != nil || !owned {
		t.Fatal(owned, err)
	}
	if _, owned, err = claimOutbox(ctx, db, "replica-event"); err != nil || owned {
		t.Fatal("simultaneous publisher claimed same event", owned, err)
	}
	if err := db.Model(&Outbox{}).Where("id = ?", "replica-event").Update("lease_until", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	second, owned, err := claimOutbox(ctx, db, "replica-event")
	if err != nil || !owned || second == first {
		t.Fatal("expired event was not reclaimed", owned, err)
	}
	for _, state := range []string{"PENDING", "PUBLISHED"} {
		if err = markOutbox(ctx, db, "replica-event", first, map[string]any{"state": state}); err == nil {
			t.Fatalf("expired publisher overwrote new claim with %s", state)
		}
	}
	if err = markOutbox(ctx, db, "replica-event", second, map[string]any{"state": "PUBLISHED", "published_at": time.Now()}); err != nil {
		t.Fatal(err)
	}
	var row Outbox
	if err = db.First(&row, "id = ?", "replica-event").Error; err != nil || row.State != "PUBLISHED" || row.Attempts != 2 {
		t.Fatal(row, err)
	}
}
