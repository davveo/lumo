package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func recallSetup(t *testing.T) (platform.Module, *gorm.DB) {
	t.Helper()
	db, err := platform.OpenDB(context.Background(), "sqlite", filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := db.DB()
	t.Cleanup(func() { _ = raw.Close() })
	m := catalog.New(platform.Dependencies{DB: db})
	if err = m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, db
}

func insertRecallProduct(
	t *testing.T,
	db *gorm.DB,
	id, name, brand, category, state, parent string,
	visible bool,
	created time.Time,
) {
	t.Helper()
	data, err := json.Marshal(
		map[string]any{
			"name":            name,
			"brand":           brand,
			"category":        category,
			"price_cents":     12345,
			"image":           "product.png",
			"catalog_visible": visible,
			"specs":           map[string]any{"material": "cotton"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Table("catalog_records").Create(
		map[string]any{
			"id":         id,
			"user_id":    "",
			"parent_id":  parent,
			"kind":       "sku",
			"status":     state,
			"payload":    string(data),
			"created_at": created,
			"updated_at": created,
		},
	).Error; err != nil {
		t.Fatal(err)
	}
}

func recallItems(t *testing.T, m platform.Module, data map[string]any) []platform.Result {
	t.Helper()
	result, err := m.Handle(
		context.Background(),
		platform.Command{Action: "recommendation_candidates", Role: "system", Data: data},
	)
	if err != nil {
		t.Fatal(err)
	}
	return result["items"].([]platform.Result)
}

func TestRecommendationRecallFindsOlderInterestsWithoutNPlusOne(t *testing.T) {
	m, db := recallSetup(t)
	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := old.AddDate(2, 0, 0)
	for i := range 120 {
		insertRecallProduct(
			t,
			db,
			fmt.Sprintf("new-%03d", i),
			"Recent Phone",
			"Common",
			"手机数码",
			"ACTIVE",
			"",
			true,
			newer.Add(time.Duration(i)*time.Minute),
		)
	}
	insertRecallProduct(t, db, "old-apple", "Apple Tablet", "Apple", "手机数码", "ACTIVE", "", true, old)
	insertRecallProduct(t, db, "old-rare", "Rare Headphones", "Rare Audio", "音频设备", "ACTIVE", "", true, old)
	insertRecallProduct(t, db, "old-search", "独特搜索词 产品", "Small Studio", "家居生活", "ACTIVE", "", true, old)
	insertRecallProduct(t, db, "disabled", "独特搜索词 产品", "Rare Audio", "手机数码", "DISABLED", "", true, newer)
	insertRecallProduct(t, db, "child", "独特搜索词 产品", "Rare Audio", "手机数码", "ACTIVE", "old-rare", true, newer)
	insertRecallProduct(t, db, "hidden", "独特搜索词 产品", "Rare Audio", "手机数码", "ACTIVE", "", false, newer)
	if err := db.Table("catalog_records").Create(
		map[string]any{
			"id":         "policy",
			"kind":       "policy",
			"parent_id":  "",
			"status":     "ACTIVE",
			"payload":    `{"brand":"Rare Audio"}`,
			"created_at": newer,
			"updated_at": newer,
		},
	).Error; err != nil {
		t.Fatal(err)
	}
	list, err := m.Handle(context.Background(), platform.Command{Action: "list", Data: map[string]any{"limit": 100}})
	if err != nil || len(list["items"].([]platform.Result)) != 100 {
		t.Fatalf("existing list changed: %v %v", list, err)
	}
	for _, row := range list["items"].([]platform.Result) {
		if strings.HasPrefix(platform.Text(row, "id"), "old-") {
			t.Fatal("old preference fixture unexpectedly in the default first page")
		}
	}
	var queries atomic.Int64
	if err := db.Callback().Query().Before("gorm:query").Register("count-recommendation-recall", func(tx *gorm.DB) {
		if !tx.DryRun {
			queries.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{
		"categories": []string{"手机数码"},
		"brands":     []string{"Apple", "Rare Audio"},
		"queries":    []string{"独特搜索词"},
		"limit":      12,
	}
	rows := recallItems(t, m, data)
	if queries.Load() != 3 || len(rows) > 12 {
		t.Fatalf("recall must execute at most three channel reads: queries=%d rows=%d", queries.Load(), len(rows))
	}
	got := map[string]bool{}
	ids := []string{}
	for _, row := range rows {
		id := platform.Text(row, "id")
		if got[id] {
			t.Fatal("duplicate candidate", id)
		}
		got[id] = true
		ids = append(ids, id)
		if platform.Int(row, "price_cents") != 12345 || platform.Text(row, "image") != "product.png" ||
			row["specs"] == nil {
			t.Fatalf("candidate metadata was truncated: %v", row)
		}
	}
	for _, id := range []string{"old-apple", "old-rare", "old-search"} {
		if !got[id] {
			t.Fatalf("popular category crowded out older preferred signal %s: %v", id, ids)
		}
	}
	for _, id := range []string{"disabled", "child", "hidden", "policy"} {
		if got[id] {
			t.Fatalf("nonpublic product leaked: %s", id)
		}
	}
	again, _ := json.Marshal(recallItems(t, m, data))
	first, _ := json.Marshal(rows)
	if string(first) != string(again) {
		t.Fatal("same recall did not return deterministic ordering")
	}
}

func TestRecommendationRecallBoundsValidationAndSystemAuthorization(t *testing.T) {
	m, db := recallSetup(t)
	queries := 0
	_ = db.Callback().Query().Before("gorm:query").Register("count-recall-empty", func(*gorm.DB) { queries++ })
	if rows := recallItems(t, m, map[string]any{}); len(rows) != 0 || queries != 0 {
		t.Fatalf("empty interests queried the whole catalog: %v queries=%d", rows, queries)
	}
	for _, role := range []string{"", "user", "admin"} {
		_, err := m.Handle(
			context.Background(),
			platform.Command{
				Action: "recommendation_candidates",
				Role:   role,
				UserID: "buyer",
				Data:   map[string]any{"categories": []string{"phone"}},
			},
		)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("non-system recall accepted: role=%q err=%v", role, err)
		}
	}
	for _, data := range []map[string]any{
		{"limit": -1}, {"limit": 201}, {"limit": "50"}, {"limit": 1.5},
		{"categories": []string{"1", "2", "3", "4", "5"}},
		{"brands": []string{strings.Repeat("长", 65)}},
		{"queries": []string{"a", "b", "c", "d"}},
		{"queries": []string{""}}, {"queries": []string{"\x00"}},
		{"queries": []string{strings.Repeat("q", 81)}},
		{"categories": "phone"}, {"unknown": true},
	} {
		_, err := m.Handle(
			context.Background(),
			platform.Command{Action: "recommendation_candidates", Role: "system", Data: data},
		)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid recall accepted: %v err=%v", data, err)
		}
	}
	if queries != 0 {
		t.Fatal("invalid or unauthorized recall ran SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.Handle(
		ctx,
		platform.Command{
			Action: "recommendation_candidates",
			Role:   "system",
			Data:   map[string]any{"categories": []string{"phone"}},
		},
	)
	if status.Code(err) != codes.Canceled {
		t.Fatalf("recall ignored caller cancellation: %v", err)
	}
}

func TestRecommendationRecallTreatsSQLAndLikeMetacharactersAsData(t *testing.T) {
	m, db := recallSetup(t)
	now := time.Now()
	insertRecallProduct(t, db, "literal", "Studio 100%_! Cotton", "Brand' OR 1=1 --", "家居生活", "ACTIVE", "", true, now)
	insertRecallProduct(t, db, "other", "Studio Other Cotton", "Other", "家居生活", "ACTIVE", "", true, now)
	for _, data := range []map[string]any{
		{"brands": []string{"Brand' OR 1=1 --"}},
		{"queries": []string{"100%_!"}},
	} {
		rows := recallItems(t, m, data)
		if len(rows) != 1 || platform.Text(rows[0], "id") != "literal" {
			t.Fatalf("query input became SQL or wildcard logic: %v rows=%v", data, rows)
		}
	}
	rows := recallItems(t, m, map[string]any{"queries": []string{"' OR 1=1 --"}})
	if len(rows) != 1 || platform.Text(rows[0], "id") != "literal" {
		t.Fatalf("query string was not matched literally: %v", rows)
	}
	rows = recallItems(t, m, map[string]any{"queries": []string{"missing%' OR 1=1 --"}})
	if len(rows) != 0 {
		t.Fatalf("injection string recalled unrelated products: %v", rows)
	}
}

func TestRecommendationRecallKeepsHealthyLaneWhenAnotherTimesOut(t *testing.T) {
	m, db := recallSetup(t)
	now := time.Now()
	insertRecallProduct(t, db, "category-match", "Cotton Shirt", "Common", "服饰箱包", "ACTIVE", "", true, now)
	insertRecallProduct(t, db, "brand-match", "Rare Headphones", "Rare", "音频设备", "ACTIVE", "", true, now)
	if err := db.Callback().Query().Before("gorm:query").Register("slow-brand-recall", func(tx *gorm.DB) {
		if strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"].Expression), "$.brand") {
			<-tx.Statement.Context.Done()
			tx.AddError(tx.Statement.Context.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := m.Handle(
		context.Background(),
		platform.Command{
			Action: "recommendation_candidates",
			Role:   "system",
			Data:   map[string]any{"categories": []string{"服饰箱包"}, "brands": []string{"Rare"}},
		},
	)
	if err != nil {
		t.Fatal("a failed optional lane discarded successful recall", err)
	}
	rows := result["items"].([]platform.Result)
	if result["degraded"] != true || len(rows) != 1 || platform.Text(rows[0], "id") != "category-match" {
		t.Fatalf("partial recall not reported or healthy rows discarded: %v", result)
	}
	if time.Since(started) > time.Second {
		t.Fatal("sparse recall did not respect its independent channel deadline")
	}
}
