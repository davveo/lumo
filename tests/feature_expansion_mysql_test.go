package tests_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type expansionMySQLRouter map[string]platform.Module

func (r expansionMySQLRouter) Call(ctx context.Context, name string, c platform.Command) (platform.Result, error) {
	module, ok := r[name]
	if !ok {
		return nil, status.Error(codes.NotFound, "test service not configured")
	}
	return module.Handle(ctx, c)
}

// Uses disposable schemas, never the application's persistent databases.
func TestMySQLFeatureExpansionTransactions(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("IAM_EMAIL_WEBHOOK_URL", "")
	t.Setenv("LOGISTICS_CARRIER_CREATE_URL", "")
	router := expansionMySQLRouter{}
	databases := map[string]*gorm.DB{}
	for _, name := range []string{"iam", "catalog", "inventory", "logistics", "analytics"} {
		ctx, db := adminMySQLScratch(t, "expansion_"+name)
		if e := platform.MigratePlatform(ctx, db); e != nil {
			t.Fatal(e)
		}
		m, e := bootstrap.New(name, platform.Dependencies{DB: db, RPC: router})
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			if e = m.Migrate(ctx); e != nil {
				t.Fatalf("%s migration: %v", name, e)
			}
		}
		if m.Seed != nil {
			if e = m.Seed(ctx); e != nil {
				t.Fatal(e)
			}
		}
		router[name] = m
		databases[name] = db
	}
	ctx := context.Background()
	call := func(service, action, id, key, user, role string, data map[string]any) platform.Result {
		t.Helper()
		v, e := router.Call(
			ctx,
			service,
			platform.Command{Action: action, ID: id, IdempotencyKey: key, UserID: user, Role: role, Data: data},
		)
		if e != nil {
			t.Fatalf("%s/%s: %v", service, action, e)
		}
		return v
	}
	login := call(
		"iam",
		"login",
		"",
		"",
		"",
		"",
		map[string]any{"username": "demo", "password": "Lumo@2026", "_client_ip": "203.0.113.20"},
	)
	challenge := call(
		"iam",
		"security_send",
		"",
		"",
		"demo",
		"user",
		map[string]any{
			"channel":          "email",
			"address":          "mysql@example.test",
			"current_password": "Lumo@2026",
			"_client_ip":       "203.0.113.20",
		},
	)
	call(
		"iam",
		"security_verify",
		"",
		"",
		"demo",
		"user",
		map[string]any{"challenge_id": challenge["challenge_id"], "code": challenge["demo_code"]},
	)
	call(
		"iam",
		"password_change",
		"",
		"",
		"demo",
		"user",
		map[string]any{"current_password": "Lumo@2026", "new_password": "Changed@2026"},
	)
	if _, e := router.Call(ctx, "iam", platform.Command{Action: "validate", Data: map[string]any{"token": login["token"]}}); status.Code(
		e,
	) != codes.Unauthenticated {
		t.Fatal("password change kept old session", e)
	}
	job := call(
		"catalog",
		"import_preview",
		"",
		"",
		"admin",
		"admin",
		map[string]any{
			"filename": "mysql.csv",
			"mode":     "create",
			"file_base64": base64.StdEncoding.EncodeToString(
				[]byte("id,name,price_cents,active\nmysql-import,MySQL product,1999,false\n"),
			),
		},
	)
	call("catalog", "import_commit", platform.Text(job, "id"), "", "admin", "admin", nil)
	if e := router["catalog"].Tick(ctx); e != nil {
		t.Fatal(e)
	}
	job = call("catalog", "import_get", platform.Text(job, "id"), "", "admin", "admin", nil)
	if platform.Int(job, "succeeded") != 1 {
		t.Fatal(job)
	}
	po := call(
		"inventory",
		"procurement_create",
		"",
		"mysql-po",
		"admin",
		"admin",
		map[string]any{
			"supplier": "MySQL supplier",
			"lines":    []map[string]any{{"sku_id": "mysql-import", "quantity": 3, "unit_cost_cents": 1000}},
		},
	)
	poID := platform.Text(po, "id")
	call("inventory", "procurement_approve", poID, "", "admin", "admin", map[string]any{"expected_version": 0})
	concurrent := func(service string, cmd platform.Command) {
		t.Helper()
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, e := router.Call(ctx, service, cmd); errs <- e }()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatalf("concurrent %s/%s: %v", service, cmd.Action, e)
			}
		}
	}
	concurrent(
		"inventory",
		platform.Command{
			Action:         "procurement_receive",
			ID:             poID,
			UserID:         "admin",
			Role:           "admin",
			IdempotencyKey: "same-receipt",
			Data: map[string]any{
				"expected_version": 1,
				"lines":            []map[string]any{{"sku_id": "mysql-import", "quantity": 2, "damaged": 1}},
			},
		},
	)
	stock := call("inventory", "get", "mysql-import", "", "admin", "admin", nil)
	if platform.Int(stock, "on_hand") != 1 {
		t.Fatal("duplicate receipt increased stock", stock)
	}
	waybill := call(
		"logistics",
		"create",
		"mysql-shipment",
		"",
		"demo",
		"system",
		map[string]any{"order_id": "mysql-order"},
	)
	concurrent(
		"logistics",
		platform.Command{
			Action: "tracking_update",
			ID:     "same-scan",
			UserID: "admin",
			Role:   "admin",
			Data: map[string]any{
				"tracking_id": waybill["tracking_id"],
				"status":      "IN_TRANSIT",
				"occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
				"description": "Concurrent MySQL scan",
			},
		},
	)
	var deliveryReads atomic.Int32
	readGate := make(chan struct{})
	logisticsDB := databases["logistics"]
	if err := logisticsDB.Callback().Query().After("gorm:query").Register("delivery_initial_read_gate", func(tx *gorm.DB) {
		if tx.Statement.Table != "records" || !strings.Contains(tx.Statement.SQL.String(), "WHERE id =") {
			return
		}
		n := deliveryReads.Add(1)
		if n == 2 {
			close(readGate)
		}
		if n <= 2 {
			select {
			case <-readGate:
			case <-time.After(5 * time.Second):
				tx.AddError(fmt.Errorf("delivery read barrier timed out"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	concurrent("logistics", platform.Command{Action: "deliver", ID: "mysql-shipment", UserID: "admin", Role: "admin"})
	if err := logisticsDB.Callback().Query().Remove("delivery_initial_read_gate"); err != nil {
		t.Fatal(err)
	}
	call("logistics", "deliver", "mysql-shipment", "", "admin", "admin", nil)
	tracking := call("logistics", "tracking", "mysql-shipment", "", "demo", "user", nil)
	var detail struct {
		Shipments []struct {
			Status string `json:"status"`
			Total  int    `json:"events_total"`
		} `json:"shipments"`
	}
	if e := platform.Decode(tracking, &detail); e != nil || len(detail.Shipments) != 1 ||
		detail.Shipments[0].Total != 3 ||
		detail.Shipments[0].Status != "DELIVERED" {
		t.Fatal(tracking, e)
	}
	event := platform.Event{
		ID:          "mysql-paid",
		Type:        "PaymentSucceeded",
		AggregateID: "mysql-order",
		UserID:      "demo",
		Data:        map[string]any{"order_id": "mysql-order", "amount_cents": 2000, "items": []any{}},
	}
	for i := 0; i < 2; i++ {
		if e := router["analytics"].OnEvent(ctx, event); e != nil {
			t.Fatal(e)
		}
	}
	metrics := call("analytics", "dashboard", "", "", "admin", "admin", map[string]any{"days": 7})
	if platform.Int(metrics, "paid_cents") != 2000 || platform.Int(metrics, "paid_orders") != 1 {
		t.Fatal(metrics)
	}
}
