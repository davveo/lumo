package platform

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTaskListExplainsBusinessFailureAndProtectsLiveWork(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDB(ctx, "sqlite", filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err = MigratePlatform(ctx, db); err != nil {
		t.Fatal(err)
	}
	envelope, _ := json.Marshal(Event{ID: "event-a", Type: constants.EventPaymentSucceeded, AggregateID: "PAY_123", UserID: "buyer", Data: map[string]any{"order_id": "ORD_123", "phone": "13800138000", "password": "never-display-this"}})
	now := time.Now().UTC()
	for _, in := range []Inbox{
		{Consumer: "order", EventID: "event-a", PayloadJSON: string(envelope), State: constants.InboxStateDead, Attempts: 5, ClaimToken: "claim", LastErrorCode: "Unavailable", LeaseUntil: now.Add(-time.Minute)},
		{Consumer: "order", EventID: "event-live", PayloadJSON: string(envelope), State: constants.InboxStateDead, Attempts: 5, LeaseUntil: now.Add(time.Minute)},
		{Consumer: "another", EventID: "event-other", PayloadJSON: string(envelope), State: constants.InboxStateDead, Attempts: 5},
		{Consumer: "order", EventID: "event-done", PayloadJSON: string(envelope), State: constants.InboxStateDone, Attempts: 1},
	} {
		if err = db.WithContext(ctx).Create(&in).Error; err != nil {
			t.Fatal(err)
		}
	}
	query := Request{UserID: "admin", Role: constants.RoleAdmin, Data: map[string]any{"kind": "inbox", "status": "attention", "keyword": "ORD_123", "page_size": 1}}
	r, err := serviceTasks(ctx, db, "order", query)
	if err != nil {
		t.Fatal(err)
	}
	if Int(r, "total") != 2 {
		t.Fatal("filters leaked another consumer or completed work", r)
	}
	query.Data["page_size"] = 20
	r, err = serviceTasks(ctx, db, "order", query)
	if err != nil {
		t.Fatal(err)
	}
	rows := r["items"].([]Result)
	for _, row := range rows {
		if row["task_title"] != "支付成功通知" || row["business_id"] != "PAY_123" {
			t.Fatal(row)
		}
		if Text(row, "id") == "event-a" && (row["retryable"] != true || row["failure_reason"] != "依赖服务暂时不可用") {
			t.Fatal(row)
		}
		if Text(row, "id") == "event-live" && row["retryable"] != false {
			t.Fatal("live lease offered retry", row)
		}
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "13800138000") || strings.Contains(string(b), "never-display-this") || strings.Contains(string(b), "payload_json") {
		t.Fatal("unnecessary payload leaked", string(b))
	}
	var failed Inbox
	if err = db.WithContext(ctx).First(&failed, "consumer = ? AND event_id = ?", "order", "event-a").Error; err != nil {
		t.Fatal(err)
	}
	if err = markInbox(ctx, db, failed, constants.InboxStateDead, status.Error(codes.FailedPrecondition, "private-data-and-credentials")); err != nil {
		t.Fatal(err)
	}
	if err = db.WithContext(ctx).First(&failed, "consumer = ? AND event_id = ?", "order", "event-a").Error; err != nil {
		t.Fatal(err)
	}
	if failed.LastErrorCode != "FailedPrecondition" || failed.LastErrorAt == nil {
		t.Fatal(failed)
	}
	if err = markInbox(ctx, db, failed, constants.InboxStateDone); err != nil {
		t.Fatal(err)
	}
	failed = Inbox{}
	if err = db.WithContext(ctx).First(&failed, "consumer = ? AND event_id = ?", "order", "event-a").Error; err != nil {
		t.Fatal(err)
	}
	if failed.LastErrorCode != "" || failed.LastErrorAt != nil {
		t.Fatal("completion retained current failure", failed)
	}
	query.Role = constants.RoleUser
	if _, err = serviceTasks(ctx, db, "order", query); status.Code(err) != codes.PermissionDenied {
		t.Fatal(err)
	}
}

func TestTaskHistoryLabelsMissingDiagnosticsAndLinksReplay(t *testing.T) {
	now := time.Now().UTC()
	payload := `{"id":"evt","type":"RefundSucceeded","aggregate_id":"RFND_123","data":{"order_id":"ORD_123"}}`
	r := describeTask(Result{"id": "evt", "state": constants.InboxStateDead}, "inbox", "order", payload, "", now)
	if r["failure_reason"] != "历史记录未保存具体失败原因" || r["task_title"] != "退款到账通知" {
		t.Fatal(r)
	}
	refs := r["business_references"].([]Result)
	if len(refs) != 2 || refs[0]["label"] != "退款单" || refs[1]["label"] != "订单" {
		t.Fatal(refs)
	}
	r = describeTask(Result{"id": "recovery", "event_id": "evt", "state": constants.InboxStateDone}, "recovery", "order", payload, "", now)
	if r["status_label"] != "重试成功" {
		t.Fatal(r)
	}
}
