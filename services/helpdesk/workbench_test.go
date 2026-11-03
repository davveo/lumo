package helpdesk_test

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/helpdesk"
	"github.com/davveo/lumo/services/helpdesk/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
	"time"
)

type workCaller struct {
	role     string
	calls    map[string]int
	failCase bool
}

func (r *workCaller) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	r.calls[service+"/"+c.Action]++
	if service == "iam" {
		role := r.role
		if c.ID == "warehouse-target" {
			role = "staff_warehouse"
		}
		return platform.Result{"id": c.ID, "role": role, "username": c.ID, "active": true}, nil
	}
	if service == "order" {
		o := platform.Result{
			"id":            "order",
			"user_id":       "buyer",
			"payable_cents": 10000,
			"address":       map[string]any{"recipient": "收货人", "phone": "13800000000", "address": "仓库配送地址"},
			"items":         []any{map[string]any{"sku_id": "sku", "quantity": 1, "unit_price_cents": 10000}},
		}
		if c.Action == "batch_get" {
			return platform.Result{"items": []any{o}}, nil
		}
		return o, nil
	}
	if service == "logistics" {
		return platform.Result{"shipments": []any{}}, nil
	}
	if service == "aftersale" && r.failCase {
		return nil, status.Error(codes.Unavailable, "case offline")
	}
	row := platform.Result{
		"id":       service + "-job",
		"order_id": "order",
		"user_id":  "buyer",
		"status": map[string]string{
			"fulfillment": "READY",
			"aftersale":   "REQUESTED",
			"payment":     "PENDING",
			"refund":      "PROCESSING",
		}[service],
		"created_at": time.Now().Add(-72 * time.Hour).UTC().Format(time.RFC3339Nano),
	}
	if c.Action == "get" {
		return row, nil
	}
	if c.Action != "admin_list" {
		return nil, status.Error(codes.Unimplemented, "unexpected work read")
	}
	if c.Data["oldest_first"] != true {
		return nil, status.Error(codes.InvalidArgument, "must read oldest jobs")
	}
	var in struct {
		IDs []string `json:"ids"`
	}
	_ = platform.Decode(c.Data, &in)
	if len(in.IDs) > 0 {
		found := false
		for _, id := range in.IDs {
			if id == platform.Text(row, "id") {
				found = true
			}
		}
		if !found {
			return platform.Result{"items": []any{}, "total": 0}, nil
		}
	}
	return platform.Result{"items": []any{row}, "total": 1}, nil
}
func workSetup(t *testing.T, role string) (platform.Module, *gorm.DB, *workCaller) {
	t.Helper()
	db, e := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "work.db"))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	if e = platform.MigratePlatform(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	rpc := &workCaller{role: role, calls: map[string]int{}}
	m := helpdesk.New(platform.Dependencies{DB: db, RPC: rpc})
	if e = m.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	return m, db, rpc
}
func workCall(t *testing.T, m platform.Module, action string, data map[string]any, key string) platform.Result {
	t.Helper()
	r, e := m.Operations[action](
		context.Background(),
		platform.Request{UserID: "operator", Role: "admin", Data: data, IdempotencyKey: key},
	)
	if e != nil {
		t.Fatal(action, e)
	}
	return r
}
func TestWorkQueueReadsOriginalStaffGrantsAndDegradesPerSource(t *testing.T) {
	m, _, rpc := workSetup(t, "staff_warehouse")
	r := workCall(t, m, "work_queue", nil, "")
	var page struct {
		Items   []platform.Result
		Allowed []string `json:"allowed_services"`
	}
	_ = platform.Decode(r, &page)
	if len(page.Items) != 1 || len(page.Allowed) != 1 || page.Allowed[0] != "fulfillment" {
		t.Fatal(r)
	}
	if rpc.calls["payment/admin_list"]+rpc.calls["aftersale/admin_list"]+rpc.calls["refund/admin_list"] != 0 {
		t.Fatal("promoted role read unrelated data", rpc.calls)
	}
	detail, e := m.Operations["work_detail"](
		context.Background(),
		platform.Request{
			ID:     "fulfillment-job",
			UserID: "operator",
			Role:   "admin",
			Data:   map[string]any{"service": "fulfillment"},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	var result struct{ Order platform.Result }
	_ = platform.Decode(detail, &result)
	if result.Order["payable_cents"] != nil || result.Order["address"] == nil {
		t.Fatal("warehouse order context not redacted", detail)
	}
	rpc.role = "user"
	_, e = m.Operations["work_queue"](context.Background(), platform.Request{UserID: "operator", Role: "admin"})
	if status.Code(e) != codes.PermissionDenied {
		t.Fatal("revoked role trusted a promoted principal", e)
	}
	rpc.role = "admin"
	rpc.failCase = true
	r = workCall(t, m, "work_queue", nil, "")
	var partial struct {
		Items   []platform.Result
		Partial bool
		Errors  map[string]string
	}
	_ = platform.Decode(r, &partial)
	if !partial.Partial || partial.Errors["aftersale"] == "" || len(partial.Items) == 0 {
		t.Fatal(r)
	}
}
func TestWorkAssignmentsReturnPerItemResultsAndReplayWithoutDuplicateAudit(t *testing.T) {
	m, db, _ := workSetup(t, "admin")
	data := map[string]any{
		"items": []any{
			map[string]any{"service": "fulfillment", "id": "fulfillment-job", "expected_version": 0},
			map[string]any{"service": "payment", "id": "missing", "expected_version": 0},
		},
		"assignee_id": "warehouse-target",
		"reason":      "处理出库",
	}
	r := workCall(t, m, "assign_work", data, "assignment")
	var page struct{ Items []platform.Result }
	_ = platform.Decode(r, &page)
	success, failed := 0, 0
	for _, v := range page.Items {
		if v["ok"] == true {
			success++
		} else {
			failed++
		}
	}
	if success != 1 || failed != 1 {
		t.Fatal(r)
	}
	workCall(t, m, "assign_work", data, "assignment")
	var count int64
	_ = db.Model(&domain.WorkChange{}).Count(&count).Error
	if count != 1 {
		t.Fatal("replay duplicated audit", count)
	}
	r = workCall(t, m, "assign_work", data, "new-request")
	_ = platform.Decode(r, &page)
	for _, v := range page.Items {
		if v["ok"] == true {
			t.Fatal("stale assignment overwritten", r)
		}
	}
	views := map[string]any{
		"name":    "我负责的出库",
		"filters": map[string]any{"service": "fulfillment", "assignee": "mine"},
		"version": 0,
	}
	workCall(t, m, "save_view", views, "")
	list := workCall(t, m, "views", nil, "")
	_ = platform.Decode(list, &page)
	if len(page.Items) != 1 {
		t.Fatal(list)
	}
	_, e := m.Operations[constants.ActionSaveView](
		context.Background(),
		platform.Request{UserID: "operator", Role: "admin", Data: views},
	)
	if status.Code(e) != codes.Aborted {
		t.Fatal("view version overwritten", e)
	}
	other, e := m.Operations["views"](context.Background(), platform.Request{UserID: "other", Role: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	_ = platform.Decode(other, &page)
	if len(page.Items) != 0 {
		t.Fatal("saved views leaked", other)
	}
}
