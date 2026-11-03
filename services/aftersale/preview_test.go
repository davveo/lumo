package aftersale

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestPreviewSharesResidualCashCalculationAndNeverReservesQuantity(t *testing.T) {
	refunded := int64(0)
	rpc := &fakeCaller{fn: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		if service != "order" {
			t.Fatalf("unexpected dependency %s", service)
		}
		return platform.Result{
			"id":                   "order",
			"user_id":              "buyer",
			"payment_id":           "pay",
			"status":               "COMPLETED",
			"payable_cents":        10,
			"refunded_total_cents": refunded,
			"quote":                map[string]any{"points": 3},
			"items": []any{
				map[string]any{"sku_id": "a", "quantity": 1, "unit_price_cents": 1},
				map[string]any{"sku_id": "b", "quantity": 1, "unit_price_cents": 1},
				map[string]any{"sku_id": "c", "quantity": 1, "unit_price_cents": 1},
			},
		}, nil
	}}
	m, db := setup(t, rpc)
	for i, sku := range []string{"a", "b", "c"} {
		data := map[string]any{
			"order_id": "order",
			"type":     "return",
			"reason":   "质量问题",
			"lines":    []any{map[string]any{"sku_id": sku, "quantity": 1}},
		}
		want := int64(3)
		if i == 2 {
			want = 4
		}
		v := run(t, m, platform.Command{Action: "preview", UserID: "buyer", Role: "user", Data: data})
		if platform.Int(v, "refund_cents") != want {
			t.Fatal(v)
		}
		var count int64
		_ = db.Model(&domain.Record{}).Count(&count).Error
		if count != int64(i) {
			t.Fatal("preview reserved quantity", count)
		}
		created := run(
			t,
			m,
			platform.Command{Action: "create", UserID: "buyer", Role: "user", IdempotencyKey: sku, Data: data},
		)
		if platform.Int(created, "amount_cents") != want {
			t.Fatal("preview differs from commit", v, created)
		}
		refunded += want
	}
}
func TestEvidenceIsVerifiedAsOneOwnedBatchAndOnlyLinkedFilesCanDownload(t *testing.T) {
	verify, download := 0, 0
	rpc := &fakeCaller{fn: func(_ context.Context, service string, c platform.Command) (platform.Result, error) {
		if service == "order" {
			return platform.Result{
				"id":            "order",
				"user_id":       "buyer",
				"payment_id":    "pay",
				"status":        "SHIPPED",
				"payable_cents": 100,
				"items":         []any{map[string]any{"sku_id": "a", "quantity": 1, "unit_price_cents": 100}},
			}, nil
		}
		if service == "media" && c.Action == "verify_files" {
			verify++
			if c.UserID != "buyer" || c.Role != "system" {
				t.Fatal(c)
			}
			return platform.Result{
				"items": []any{
					map[string]any{"file_id": "file-a", "file_name": "包装.jpg"},
					map[string]any{"file_id": "file-b", "file_name": "破损.jpg"},
				},
			}, nil
		}
		if service == "media" && c.Action == "download" {
			download++
			if c.UserID != "buyer" || c.ID != "file-a" {
				t.Fatal(c)
			}
			return platform.Result{"download_url": "https://objects.test/image"}, nil
		}
		t.Fatal(service, c.Action)
		return nil, nil
	}}
	m, _ := setup(t, rpc)
	data := map[string]any{
		"order_id":     "order",
		"type":         "return",
		"reason":       "质量问题",
		"evidence_ids": []string{"file-a", "file-b"},
	}
	v := run(
		t,
		m,
		platform.Command{Action: "create", UserID: "buyer", Role: "user", IdempotencyKey: "case", Data: data},
	)
	if verify != 1 {
		t.Fatal(verify)
	}
	var linked []map[string]any
	if e := json.Unmarshal([]byte(platform.Text(v, "evidence_json")), &linked); e != nil || len(linked) != 2 {
		t.Fatal(v, e)
	}
	id := platform.Text(v, "id")
	run(
		t,
		m,
		platform.Command{
			Action: "evidence_download",
			ID:     id,
			UserID: "staff",
			Role:   "admin",
			Data:   map[string]any{"file_id": "file-a"},
		},
	)
	_, e := m.Handle(
		context.Background(),
		platform.Command{
			Action: "evidence_download",
			ID:     id,
			UserID: "buyer",
			Role:   "user",
			Data:   map[string]any{"file_id": "other-file"},
		},
	)
	if status.Code(e) != codes.PermissionDenied || download != 1 {
		t.Fatal(e, download)
	}
	_, e = m.Handle(
		context.Background(),
		platform.Command{
			Action: "evidence_download",
			ID:     id,
			UserID: "other",
			Role:   "user",
			Data:   map[string]any{"file_id": "file-a"},
		},
	)
	if status.Code(e) != codes.PermissionDenied || download != 1 {
		t.Fatal(e, download)
	}
}
