package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/bootstrap"
	"github.com/davveo/lumo/internal/platform"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

// This harness uses 30 isolated SQLite databases and real TCP gRPC servers.
// Its deterministic outbox pump validates domain event integration, not a live
// RabbitMQ broker. The Docker smoke script exercises the external MQ topology.
type cluster struct {
	rpc      platform.Caller
	modules  map[string]platform.Module
	dbs      map[string]*gorm.DB
	exporter *tracetest.InMemoryExporter
}

func newCluster(t *testing.T) *cluster {
	t.Helper()
	t.Setenv("DEMO_MODE", "true")
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()); otel.SetTracerProvider(previous) })
	listeners := map[string]net.Listener{}
	targets := map[string]string{}
	for _, name := range bootstrap.Names {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listeners[name] = listener
		targets[name] = listener.Addr().String()
		t.Cleanup(func() { _ = listener.Close() })
	}
	c := &cluster{
		rpc:      platform.NewCaller(targets, "integration-service-secret"),
		modules:  map[string]platform.Module{},
		dbs:      map[string]*gorm.DB{},
		exporter: exporter,
	}
	if closer, ok := c.rpc.(interface{ Close() error }); ok {
		t.Cleanup(func() { _ = closer.Close() })
	}
	for _, name := range bootstrap.Names {
		db, err := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), name+".db"))
		if err != nil {
			t.Fatal(name, err)
		}
		sqlDB, _ := db.DB()
		t.Cleanup(func() { _ = sqlDB.Close() })
		if err = platform.MigratePlatform(context.Background(), db); err != nil {
			t.Fatal(name, err)
		}
		m, err := bootstrap.New(name, platform.Dependencies{DB: db, RPC: c.rpc})
		if err != nil {
			t.Fatal(err)
		}
		if m.Migrate != nil {
			if err = m.Migrate(context.Background()); err != nil {
				t.Fatal(name, err)
			}
		}
		if m.Seed != nil {
			if err = m.Seed(context.Background()); err != nil {
				t.Fatal(name, err)
			}
		}
		server, err := platform.NewRPCServer("integration-service-secret", m)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(server.Stop)
		go func(s *grpc.Server, l net.Listener) { _ = s.Serve(l) }(server, listeners[name])
		c.modules[name] = m
		c.dbs[name] = db
	}
	exporter.Reset()
	return c
}

func (c *cluster) call(
	t *testing.T,
	ctx context.Context,
	service, action, id, key, role, user string,
	data map[string]any,
) platform.Result {
	t.Helper()
	out, err := c.rpc.Call(
		ctx,
		service,
		platform.Command{Action: action, ID: id, IdempotencyKey: key, UserID: user, Role: role, Data: data},
	)
	if err != nil {
		t.Fatalf("%s.%s: %v", service, action, err)
	}
	return out
}
func (c *cluster) pump(t *testing.T) {
	t.Helper()
	for round := 0; round < 80; round++ {
		count := 0
		for _, producer := range bootstrap.Names {
			var rows []platform.Outbox
			if err := c.dbs[producer].Where("state = ?", "PENDING").Order("created_at ASC").Find(
				&rows,
			).Error; err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				count++
				var e platform.Event
				if err := json.Unmarshal([]byte(row.PayloadJSON), &e); err != nil {
					t.Fatal(err)
				}
				carrier := propagation.MapCarrier{}
				_ = json.Unmarshal([]byte(row.CarrierJSON), &carrier)
				for _, consumer := range bootstrap.Names {
					m := c.modules[consumer]
					if m.OnEvent == nil {
						continue
					}
					match := false
					for _, topic := range m.Topics {
						if topic == e.Type || topic == "#" {
							match = true
						}
					}
					if !match {
						continue
					}
					ctx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
					ctx, span := otel.Tracer("integration/broker").
						Start(ctx, "mq.consume."+e.Type, trace.WithSpanKind(trace.SpanKindConsumer))
					err := m.OnEvent(ctx, e)
					span.End()
					if err != nil {
						t.Fatalf("event %s from %s -> %s: %v", e.Type, producer, consumer, err)
					}
				}
				if err := c.dbs[producer].Model(&platform.Outbox{}).Where("id = ?", row.ID).Update(
					"state",
					"PUBLISHED",
				).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
		if count == 0 {
			return
		}
	}
	t.Fatal("domain event pump did not converge")
}

func TestCommerceRPCAndTrace(t *testing.T) {
	c := newCluster(t)
	ctx, root := otel.Tracer("integration").Start(context.Background(), "HTTP POST /checkout/submit")
	defer root.End()
	login := c.call(t, ctx, "iam", "login", "", "", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"})
	token := platform.Text(login, "token")
	if token == "" {
		t.Fatalf("login returned no token: %#v", login)
	}
	verified := c.call(t, ctx, "iam", "validate", "", "", "", "", map[string]any{"token": token})
	if platform.Text(verified, "user_id") != "demo" {
		t.Fatalf("wrong login principal: %#v", verified)
	}
	c.call(
		t,
		ctx,
		"cart",
		"add",
		"",
		"cart-add-1",
		"user",
		"demo",
		map[string]any{"sku_id": "sku-watch", "quantity": 1},
	)
	quote := c.call(
		t,
		ctx,
		"pricing",
		"quote",
		"",
		"",
		"user",
		"demo",
		map[string]any{
			"items":     []any{map[string]any{"sku_id": "sku-watch", "quantity": 1}},
			"coupon_id": "coupon-demo-100",
			"points":    500,
		},
	)
	quoteID := platform.Text(quote, "quote_id")
	if quoteID == "" {
		t.Fatalf("missing quote id: %#v", quote)
	}
	placed := c.call(
		t,
		ctx,
		"checkout",
		"submit",
		"",
		"checkout-integration-1",
		"user",
		"demo",
		map[string]any{
			"quote_id": quoteID,
			"address": map[string]any{
				"recipient": "Demo",
				"phone":     "00000000000",
				"address":   "Demo street",
				"city":      "Shenzhen",
			},
		},
	)
	orderID := platform.Text(placed, "order_id")
	if orderID == "" {
		t.Fatalf("missing order: %#v", placed)
	}
	again := c.call(
		t,
		ctx,
		"checkout",
		"submit",
		"",
		"checkout-integration-1",
		"user",
		"demo",
		map[string]any{
			"quote_id": quoteID,
			"address": map[string]any{
				"recipient": "Demo",
				"phone":     "00000000000",
				"address":   "Demo street",
				"city":      "Shenzhen",
			},
		},
	)
	if platform.Text(again, "order_id") != orderID {
		t.Fatal("idempotent submit created a different order")
	}
	_, changedErr := c.rpc.Call(
		ctx,
		"checkout",
		platform.Command{
			Action:         "submit",
			IdempotencyKey: "checkout-integration-1",
			UserID:         "demo",
			Role:           "user",
			Data: map[string]any{
				"quote_id": quoteID,
				"address":  map[string]any{"street": "Different address"},
			},
		},
	)
	if status.Code(changedErr) != codes.AlreadyExists {
		t.Fatalf("changed idempotency payload was accepted: %v", changedErr)
	}
	payment := c.call(t, ctx, "payment", "create", orderID, "payment-integration-1", "user", "demo", nil)
	paymentID := platform.Text(payment, "id")
	if paymentID == "" {
		paymentID = platform.Text(payment, "payment_id")
	}
	if paymentID == "" {
		t.Fatalf("missing payment: %#v", payment)
	}
	c.call(t, ctx, "payment", "confirm_demo", paymentID, "", "admin", "admin", nil)
	c.pump(t)
	order := c.call(t, ctx, "order", "get", orderID, "", "user", "demo", nil)
	if platform.Text(order, "status") != "READY_TO_FULFILL" {
		t.Fatalf("not ready after paid saga: %#v", order)
	}
	stock := c.call(t, ctx, "inventory", "get", "sku-watch", "", "user", "demo", nil)
	if platform.Int(stock, "allocated") != 1 {
		t.Fatalf("stock not allocated: %#v", stock)
	}
	inv := c.call(
		t,
		ctx,
		"invoice",
		"create",
		"",
		"invoice-integration-1",
		"user",
		"demo",
		map[string]any{"order_id": orderID, "title": "Demo invoice", "kind": "personal"},
	)
	invoiceID := platform.Text(inv, "id")
	issued := c.call(t, ctx, "invoice", "issue_demo", invoiceID, "", "admin", "admin", nil)
	originalNumber := platform.Text(issued, "number")
	if originalNumber == "" {
		t.Fatal("demo invoice missing original number")
	}
	money := platform.Int(order, "payable_cents")
	refund := c.call(
		t,
		ctx,
		"refund",
		"create",
		"refund-integration-1",
		"",
		"system",
		"demo",
		map[string]any{
			"order_id":     orderID,
			"payment_id":   paymentID,
			"amount_cents": money,
			"reason":       "integration full refund",
		},
	)
	refundID := platform.Text(refund, "id")
	if refundID == "" {
		refundID = "refund-integration-1"
	}
	c.call(t, ctx, "refund", "confirm_demo", refundID, "", "admin", "admin", nil)
	c.pump(t)
	after := c.call(t, ctx, "order", "get", orderID, "", "user", "demo", nil)
	if platform.Int(after, "refunded_cents") != money && platform.Int(after, "refunded_total_cents") != money {
		t.Fatalf("refund not projected: %#v", after)
	}
	pts := c.call(t, ctx, "points", "get", "", "", "user", "demo", nil)
	if platform.Int(pts, "available") != 5000 {
		t.Fatalf("spent points not restored: %#v", pts)
	}
	retained := c.call(t, ctx, "invoice", "get", invoiceID, "", "user", "demo", nil)
	if platform.Text(retained, "number") != originalNumber {
		t.Fatal("refund destroyed original issued invoice")
	}
	reversals := c.call(t, ctx, "invoice", "reversals", invoiceID, "", "user", "demo", nil)
	b, _ := json.Marshal(reversals["items"])
	var rev []map[string]any
	_ = json.Unmarshal(b, &rev)
	if len(rev) != 1 {
		t.Fatalf("refund must create exactly one linked invoice adjustment: %#v", reversals)
	}
	_, err := c.rpc.Call(
		ctx,
		"order",
		platform.Command{Action: "get", ID: orderID, UserID: "another-user", Role: "user"},
	)
	if status.Code(err) != codes.PermissionDenied && status.Code(err) != codes.NotFound {
		t.Fatalf("cross-account access allowed: %v", err)
	}
	c.call(t, ctx, "iam", "logout", "", "", "user", "demo", map[string]any{"token": token})
	_, err = c.rpc.Call(ctx, "iam", platform.Command{Action: "validate", Data: map[string]any{"token": token}})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("logout did not revoke token: %v", err)
	}
	root.End()
	spans := c.exporter.GetSpans()
	layers := map[string]bool{}
	rpc := false
	sql := false
	mq := false
	traceID := root.SpanContext().TraceID()
	correlatedCount := 0
	for _, s := range spans {
		if s.SpanContext.TraceID() != traceID {
			continue
		}
		correlatedCount++
		if strings.Contains(s.Name, "rpc.client") {
			rpc = true
		}
		if strings.HasPrefix(s.Name, "SQL ") {
			sql = true
		}
		if strings.HasPrefix(s.Name, "mq.consume") {
			mq = true
		}
		for _, a := range s.Attributes {
			if string(a.Key) == "code.layer" {
				layers[a.Value.AsString()] = true
			}
		}
		if strings.HasPrefix(s.Name, "SQL ") && !s.Parent.IsValid() {
			t.Errorf("orphan SQL span: %s", s.Name)
		}
	}
	if !rpc || !sql || !mq || !layers["usecase"] || !layers["repository"] {
		t.Fatalf("trace missing layers rpc=%v sql=%v mq=%v layers=%v spans=%d", rpc, sql, mq, layers, len(spans))
	}
	if os.Getenv("UPDATE_TRACE_FIXTURE") == "1" {
		writeTraceFixture(t, spans, traceID)
	}
	t.Log(
		fmt.Sprintf(
			"30 services; actual gRPC; payment/refund saga; %d correlated spans; layers=%v",
			correlatedCount,
			layers,
		),
	)
}

// The checked-in example is exported from a real passing integration run. It
// contains only generated demo data and parameterized SQL, never credentials.
func writeTraceFixture(t *testing.T, spans tracetest.SpanStubs, id trace.TraceID) {
	t.Helper()
	byID := map[trace.SpanID]tracetest.SpanStub{}
	known := map[string]bool{}
	for _, s := range bootstrap.Names {
		known[s] = true
	}
	for _, s := range spans {
		byID[s.SpanContext.SpanID()] = s
	}
	var service func(tracetest.SpanStub, int) string
	service = func(s tracetest.SpanStub, depth int) string {
		if depth > 20 {
			return "gateway"
		}
		parts := strings.Split(s.Name, ".")
		for _, p := range parts {
			if known[p] {
				return p
			}
		}
		if p, ok := byID[s.Parent.SpanID()]; ok {
			return service(p, depth+1)
		}
		return "gateway"
	}
	processes := map[string]any{}
	rows := []any{}
	for _, s := range spans {
		if s.SpanContext.TraceID() != id {
			continue
		}
		name := service(s, 0)
		processes[name] = map[string]any{"serviceName": name, "tags": []any{}}
		tags := []any{}
		for _, a := range s.Attributes {
			tags = append(
				tags,
				map[string]any{"key": string(a.Key), "type": "string", "value": fmt.Sprint(a.Value.AsInterface())},
			)
		}
		refs := []any{}
		if s.Parent.IsValid() {
			refs = append(
				refs,
				map[string]any{"refType": "CHILD_OF", "traceID": id.String(), "spanID": s.Parent.SpanID().String()},
			)
		}
		duration := s.EndTime.Sub(s.StartTime).Microseconds()
		if duration < 1 {
			duration = 1
		}
		rows = append(
			rows,
			map[string]any{
				"traceID":       id.String(),
				"spanID":        s.SpanContext.SpanID().String(),
				"operationName": s.Name,
				"references":    refs,
				"startTime":     s.StartTime.UnixMicro(),
				"duration":      duration,
				"tags":          tags,
				"logs":          []any{},
				"processID":     name,
				"warnings":      nil,
			},
		)
	}
	out := map[string]any{
		"data":   []any{map[string]any{"traceID": id.String(), "spans": rows, "processes": processes, "warnings": nil}},
		"total":  1,
		"limit":  0,
		"offset": 0,
		"errors": nil,
	}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "docs", "examples")
	if err = os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(path, "trace-example.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
}

func shipping() map[string]any {
	return map[string]any{"recipient": "Demo", "phone": "00000000000", "address": "Demo street", "city": "Shenzhen"}
}
func (c *cluster) place(t *testing.T, ctx context.Context, sku, user, key, group string) string {
	q := c.call(
		t,
		ctx,
		"pricing",
		"quote",
		"",
		"",
		"user",
		user,
		map[string]any{"items": []any{map[string]any{"sku_id": sku, "quantity": 1}}, "group_id": group},
	)
	r := c.call(
		t,
		ctx,
		"checkout",
		"submit",
		"",
		key,
		"user",
		user,
		map[string]any{"quote_id": platform.Text(q, "quote_id"), "address": shipping()},
	)
	return platform.Text(r, "order_id")
}
func (c *cluster) pay(t *testing.T, ctx context.Context, id, user string) {
	p := c.call(t, ctx, "payment", "create", id, "pay:"+id, "user", user, nil)
	paymentID := platform.Text(p, "id")
	if paymentID == "" {
		paymentID = platform.Text(p, "payment_id")
	}
	c.call(t, ctx, "payment", "confirm_demo", paymentID, "", "admin", "admin", nil)
	c.pump(t)
}
func TestCancelReleasesReservationsAndFreezesPayment(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	id := c.place(t, ctx, "sku-coffee", "demo", "cancel-flow-1", "")
	s := c.call(t, ctx, "inventory", "get", "sku-coffee", "", "user", "demo", nil)
	if platform.Int(s, "reserved") != 1 {
		t.Fatalf("missing reservation %#v", s)
	}
	c.call(t, ctx, "checkout", "cancel", id, "", "user", "demo", nil)
	c.pump(t)
	s = c.call(t, ctx, "inventory", "get", "sku-coffee", "", "user", "demo", nil)
	if platform.Int(s, "reserved") != 0 || platform.Int(s, "available") != 50 {
		t.Fatalf("cancel leaked inventory %#v", s)
	}
	_, err := c.rpc.Call(ctx, "payment", platform.Command{Action: "create", ID: id, UserID: "demo", Role: "user"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("closed order accepted payment: %v", err)
	}
}
func TestGroupWaitsForAllPaidMembersThenReleasesBoth(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	team := c.call(
		t,
		ctx,
		"group",
		"create",
		"",
		"group-flow-1",
		"user",
		"demo",
		map[string]any{"sku_id": "sku-headphones", "required": 2},
	)
	gid := platform.Text(team, "id")
	first := c.place(t, ctx, "sku-headphones", "demo", "group-order-1", gid)
	c.pay(t, ctx, first, "demo")
	row := c.call(t, ctx, "order", "get", first, "", "user", "demo", nil)
	if platform.Text(row, "status") != "PAID_WAIT_GROUP" {
		t.Fatalf("first member incorrectly released: %#v", row)
	}
	second := c.place(t, ctx, "sku-headphones", "admin", "group-order-2", gid)
	c.pay(t, ctx, second, "admin")
	for _, v := range []struct{ id, user string }{{first, "demo"}, {second, "admin"}} {
		row = c.call(t, ctx, "order", "get", v.id, "", "user", v.user, nil)
		if platform.Text(row, "status") != "READY_TO_FULFILL" {
			t.Fatalf("formed group did not release member %s: %#v", v.user, row)
		}
	}
	stock := c.call(t, ctx, "inventory", "get", "sku-headphones", "", "user", "demo", nil)
	if platform.Int(stock, "allocated") != 2 || platform.Int(stock, "reserved") != 0 {
		t.Fatalf("group allocation incorrect: %#v", stock)
	}
}
