package coupon_test

import (
	"context"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func templateData() map[string]any {
	return map[string]any{
		"name":         "后台新建券",
		"amount_cents": int64(1200),
		"min_cents":    int64(10000),
		"valid_days":   int64(7),
		"remaining":    int64(10),
		"active":       true,
		"starts_at":    nil,
		"expires_at":   time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		"min_points":   int64(0),
		"min_growth":   int64(0),
		"version":      int64(0),
	}
}
func templateEdit(saved platform.Result) map[string]any {
	return map[string]any{
		"name":         saved["name"],
		"amount_cents": saved["amount_cents"],
		"min_cents":    saved["min_cents"],
		"valid_days":   saved["valid_days"],
		"active":       saved["active"],
		"starts_at":    saved["starts_at"],
		"expires_at":   saved["expires_at"],
		"min_points":   saved["min_points"],
		"min_growth":   saved["min_growth"],
		"version":      saved["version"],
		"stock_delta":  int64(0),
	}
}
func TestTemplateManagementIsAdminOnly(t *testing.T) {
	_, module := setup(t, nil)
	for _, action := range []string{constants.ActionTemplatesAdmin, constants.ActionTemplateUpsert} {
		for _, principal := range [][2]string{{"", ""}, {"demo", constants.RoleUser}, {"worker", constants.RoleSystem}} {
			_, err := invoke(module, action, "admin-coupon", principal[0], principal[1], templateData())
			if status.Code(err) != codes.Unauthenticated && status.Code(err) != codes.PermissionDenied {
				t.Fatalf("%s accepted %v: %v", action, principal, err)
			}
		}
	}
}
func TestTemplateAdminCreatesPagesDisablesAndKeepsHistoricalCoupons(t *testing.T) {
	_, module := setup(t, nil)
	created := must(
		t,
		module,
		constants.ActionTemplateUpsert,
		"admin-test",
		"admin",
		constants.RoleAdmin,
		templateData(),
	)
	if platform.Int(created, "version") != 1 || platform.Int(created, "remaining") != 10 {
		t.Fatal(created)
	}
	claimed := must(
		t,
		module,
		constants.ActionClaim,
		"",
		"demo",
		constants.RoleUser,
		map[string]any{"template_id": "admin-test"},
	)
	rows := must(
		t,
		module,
		constants.ActionTemplatesAdmin,
		"",
		"admin",
		constants.RoleAdmin,
		map[string]any{"keyword": "admin-test", "page_size": 1},
	)
	item := rows["items"].([]platform.Result)[0]
	if platform.Int(item, "claimed_count") != 1 || platform.Int(item, "remaining") != 9 ||
		platform.Int(rows, "total") != 1 {
		t.Fatal(rows)
	}
	edit := templateEdit(item)
	edit["active"] = false
	edit["stock_delta"] = int64(3)
	edit["name"] = "停止领取但保留旧券"
	updated := must(t, module, constants.ActionTemplateUpsert, "admin-test", "admin", constants.RoleAdmin, edit)
	if platform.Int(updated, "remaining") != 12 || platform.Int(updated, "version") != 2 {
		t.Fatal(updated)
	}
	public := must(t, module, constants.ActionTemplates, "", "", "", nil)
	for _, row := range public["items"].([]platform.Result) {
		if platform.Text(row, "id") == "admin-test" {
			t.Fatal("disabled template publicly listed", row)
		}
	}
	admin := must(
		t,
		module,
		constants.ActionTemplatesAdmin,
		"",
		"admin",
		constants.RoleAdmin,
		map[string]any{"active": "false"},
	)
	if platform.Int(admin, "total") != 1 {
		t.Fatal(admin)
	}
	if _, err := invoke(module, constants.ActionClaim, "", "new-user", constants.RoleUser, map[string]any{"template_id": "admin-test"}); status.Code(
		err,
	) != codes.FailedPrecondition {
		t.Fatal(err)
	}
	old := must(
		t,
		module,
		constants.ActionClaim,
		"",
		"demo",
		constants.RoleUser,
		map[string]any{"template_id": "admin-test"},
	)
	if old["id"] != claimed["id"] || platform.Int(old, "amount_cents") != 1200 ||
		platform.Int(old, "min_cents") != 10000 {
		t.Fatal(old, claimed)
	}
}
func TestTemplateAdminLocksIssuedBenefitsAndRejectsStaleStockReset(t *testing.T) {
	_, module := setup(t, nil)
	created := must(t, module, constants.ActionTemplateUpsert, "issued", "admin", constants.RoleAdmin, templateData())
	staleStock := templateEdit(created)
	staleStock["stock_delta"] = int64(5)
	must(t, module, constants.ActionClaim, "", "demo", constants.RoleUser, map[string]any{"template_id": "issued"})
	updated := must(t, module, constants.ActionTemplateUpsert, "issued", "admin", constants.RoleAdmin, staleStock)
	if platform.Int(updated, "remaining") != 14 {
		t.Fatalf("claim consumed stock was overwritten: %v", updated)
	}
	for _, field := range []string{"amount_cents", "min_cents", "valid_days"} {
		change := templateEdit(updated)
		change[field] = platform.Int(updated, field) + 1
		if _, err := invoke(module, constants.ActionTemplateUpsert, "issued", "admin", constants.RoleAdmin, change); status.Code(
			err,
		) != codes.FailedPrecondition {
			t.Fatalf("issued %s edited: %v", field, err)
		}
	}
	if _, err := invoke(module, constants.ActionTemplateUpsert, "issued", "admin", constants.RoleAdmin, staleStock); status.Code(
		err,
	) != codes.Aborted {
		t.Fatalf("stale update accepted: %v", err)
	}
	reset := templateEdit(updated)
	reset["remaining"] = int64(500)
	if _, err := invoke(module, constants.ActionTemplateUpsert, "issued", "admin", constants.RoleAdmin, reset); status.Code(
		err,
	) != codes.InvalidArgument {
		t.Fatal("stock reset accepted", err)
	}
	negative := templateEdit(updated)
	negative["stock_delta"] = int64(-15)
	if _, err := invoke(module, constants.ActionTemplateUpsert, "issued", "admin", constants.RoleAdmin, negative); status.Code(
		err,
	) != codes.InvalidArgument {
		t.Fatal("negative remaining accepted", err)
	}
}
func TestTemplateAdminRejectsInvalidTypesRangesAndWindows(t *testing.T) {
	_, module := setup(t, nil)
	for _, change := range []map[string]any{{"name": " "},
		{"active": nil},
		{"active": "true"}, {"amount_cents": 0}, {"amount_cents": 1.5}, {"amount_cents": "123"}, {"remaining": -1}, {"valid_days": 3661}, {"min_points": -1}, {"expires_at": "invalid"}, {"expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339)}, {"starts_at": time.Now().Add(48 * time.Hour).Format(time.RFC3339)}, {"stock_delta": 1}, {"amount_cents": int64(9007199254740992)}} {
		data := templateData()
		for key, value := range change {
			data[key] = value
		}
		if _, err := invoke(module, constants.ActionTemplateUpsert, "invalid", "admin", constants.RoleAdmin, data); status.Code(
			err,
		) != codes.InvalidArgument {
			t.Fatalf("invalid %v accepted: %v", change, err)
		}
	}
	for _, query := range []map[string]any{{"page": -1}, {"page": 1.5}, {"page_size": 101}, {"active": "hidden"}} {
		if _, err := invoke(module, constants.ActionTemplatesAdmin, "", "admin", constants.RoleAdmin, query); status.Code(
			err,
		) != codes.InvalidArgument {
			t.Fatalf("bad page %v: %v", query, err)
		}
	}
}

func TestTemplateAdminMigrationProtectsPreexistingIDsFromCreate(t *testing.T) {
	db, module := setup(t, nil)
	if err := db.Table("templates").Where("id = ?", "welcome-100").Update("version", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := module.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows := must(
		t,
		module,
		constants.ActionTemplatesAdmin,
		"",
		"admin",
		constants.RoleAdmin,
		map[string]any{"keyword": "welcome-100"},
	)
	before := rows["items"].([]platform.Result)[0]
	if platform.Int(before, "version") != 1 {
		t.Fatal("legacy version not initialized", before)
	}
	if _, err := invoke(module, constants.ActionTemplateUpsert, "welcome-100", "admin", constants.RoleAdmin, templateData()); status.Code(
		err,
	) != codes.Aborted {
		t.Fatal("create overwrote existing template", err)
	}
	after := must(t, module, constants.ActionTemplatesAdmin, "", "admin", constants.RoleAdmin, map[string]any{"keyword": "welcome-100"})["items"].([]platform.Result)[0]
	if after["name"] != before["name"] || platform.Int(after, "remaining") != platform.Int(before, "remaining") ||
		platform.Int(after, "amount_cents") != platform.Int(before, "amount_cents") {
		t.Fatal(before, after)
	}
}
