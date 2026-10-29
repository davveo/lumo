package campaign_test

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func managementRule() map[string]any {
	return map[string]any{
		"kind": constants.CampaignKindReduction, "name": "运营满减", "description": "活动说明",
		"threshold_cents": int64(10000), "discount_cents": int64(1000), "pay_rate_bps": int64(0),
		"min_quantity": int64(1), "quota": int64(5), "active": true, "version": int64(0),
	}
}
func TestCampaignManagementRequiresAdminAndPreservesPublicVisibility(t *testing.T) {
	_, module := setup(t, nil)
	for _, role := range []string{"", constants.RoleUser, constants.RoleSystem} {
		_, err := invoke(module, constants.ActionUpsert, "managed", "caller", role, managementRule())
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("role %q write: %v", role, err)
		}
		_, err = invoke(module, constants.ActionAdminList, "", "caller", role, nil)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("role %q read: %v", role, err)
		}
	}
	data := managementRule()
	data["active"] = false
	created := must(t, module, constants.ActionUpsert, "managed", "admin", constants.RoleAdmin, data)
	if platform.Int(created, "version") != 1 {
		t.Fatal(created)
	}
	list := must(
		t,
		module,
		constants.ActionAdminList,
		"",
		"admin",
		constants.RoleAdmin,
		map[string]any{"keyword": "managed", "page_size": 1},
	)
	if platform.Int(list, "total") != 1 {
		t.Fatal(list)
	}
	public := must(t, module, constants.ActionList, "", "", "", nil)
	raw, _ := json.Marshal(public["items"])
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row["id"] == "managed" {
			t.Fatal("paused rule leaked into public list")
		}
	}
}

func TestCampaignAdminRuleLifecycleQuotaCountersAndFrozenBenefits(t *testing.T) {
	db, module := setup(t, nil)
	data := managementRule()
	created := must(t, module, constants.ActionUpsert, "managed", "admin", constants.RoleAdmin, data)
	if platform.Int(created, "version") != 1 {
		t.Fatal(created)
	}
	preview := must(t, module, constants.ActionEvaluate, "", "member", constants.RoleUser,
		map[string]any{"activity_id": "managed", "subtotal_cents": int64(15000), "quantity": int64(1)})
	if preview["eligible"] != true || platform.Int(preview, "discount_cents") != 1000 {
		t.Fatal(preview)
	}
	must(t, module, constants.ActionReserve, "held-order", "member", constants.RoleSystem,
		map[string]any{"activity_id": "managed", "quantity": int64(1)})
	data["version"] = int64(1)
	data["discount_cents"] = int64(2000)
	_, err := invoke(module, constants.ActionUpsert, "managed", "admin", constants.RoleAdmin, data)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("changed held benefit: %v", err)
	}
	data["discount_cents"] = int64(1000)
	data["name"] = "已停用的运营活动"
	data["active"] = false
	data["held"] = int64(0)
	data["used"] = int64(9999)
	updated := must(t, module, constants.ActionUpsert, "managed", "admin", constants.RoleAdmin, data)
	if platform.Int(updated, "held") != 1 || platform.Int(updated, "used") != 0 ||
		platform.Int(updated, "version") != 2 {
		t.Fatal(updated)
	}
	preview = must(t, module, constants.ActionEvaluate, "", "new-member", constants.RoleUser,
		map[string]any{"activity_id": "managed", "subtotal_cents": int64(15000), "quantity": int64(1)})
	if preview["eligible"] != false || preview["reason"] != "inactive" {
		t.Fatal(preview)
	}
	// Pausing blocks new reservations but does not strand an existing order.
	must(t, module, constants.ActionConfirm, "held-order", "member", constants.RoleSystem, nil)
	data["version"] = int64(2)
	data["quota"] = int64(2)
	updated = must(t, module, constants.ActionUpsert, "managed", "admin", constants.RoleAdmin, data)
	if platform.Int(updated, "held") != 0 || platform.Int(updated, "used") != 1 {
		t.Fatal(updated)
	}
	if err := db.WithContext(context.Background()).Table("activities").Where("id = ?", "managed").Updates(map[string]any{"held": 1, "used": 1}).Error; err != nil {
		t.Fatal(err)
	}
	data["version"] = int64(3)
	data["quota"] = int64(1)
	_, err = invoke(module, constants.ActionUpsert, "managed", "admin", constants.RoleAdmin, data)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("quota below committed counters: %v", err)
	}
}

func TestCampaignAdminRejectsStaleVersionsAndCannotRepurposeSystemRules(t *testing.T) {
	_, module := setup(t, nil)
	data := managementRule()
	must(t, module, constants.ActionUpsert, "managed", "admin-a", constants.RoleAdmin, data)
	_, err := invoke(module, constants.ActionUpsert, "managed", "admin-b", constants.RoleAdmin, data)
	if status.Code(err) != codes.Aborted {
		t.Fatalf("stale create overwrote activity: %v", err)
	}
	data["version"] = int64(1)
	data["active"] = false
	must(t, module, constants.ActionUpsert, "managed", "admin-a", constants.RoleAdmin, data)
	_, err = invoke(module, constants.ActionUpsert, "managed", "admin-b", constants.RoleAdmin, data)
	if status.Code(err) != codes.Aborted {
		t.Fatalf("stale update: %v", err)
	}
	data["version"] = int64(1)
	_, err = invoke(module, constants.ActionUpsert, "flash-demo", "admin", constants.RoleAdmin, data)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("system quota repurposed: %v", err)
	}
}

func TestCampaignAdminValidatesEachRuleAndPagesInactiveRecords(t *testing.T) {
	_, module := setup(t, nil)
	for _, invalid := range []map[string]any{
		{"kind": constants.CampaignKindFlash}, {"quota": int64(0)}, {"quota": int64(1000000001)},
		{"active": nil}, {"pay_rate_bps": int64(9000)}, {"discount_cents": int64(0)},
		{"kind": constants.CampaignKindDiscount, "discount_cents": int64(0), "pay_rate_bps": int64(10000)},
		{"kind": constants.CampaignKindQuantityDiscount, "discount_cents": int64(0), "pay_rate_bps": int64(8500), "min_quantity": int64(1)},
	} {
		data := managementRule()
		for key, value := range invalid {
			data[key] = value
		}
		_, err := invoke(module, constants.ActionUpsert, "invalid", "admin", constants.RoleAdmin, data)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("accepted %v: %v", invalid, err)
		}
	}
	for _, kind := range []string{constants.CampaignKindDiscount, constants.CampaignKindQuantityDiscount} {
		data := managementRule()
		data["kind"] = kind
		data["discount_cents"] = int64(0)
		data["pay_rate_bps"] = int64(8500)
		data["min_quantity"] = int64(2)
		data["active"] = false
		must(t, module, constants.ActionUpsert, "managed-"+kind, "admin", constants.RoleAdmin, data)
	}
	list := must(t, module, constants.ActionAdminList, "", "admin", constants.RoleAdmin,
		map[string]any{"active": false, "keyword": "managed-", "page": int64(2), "page_size": int64(1)})
	if platform.Int(list, "total") != 2 || platform.Int(list, "page") != 2 {
		t.Fatal(list)
	}
}

func TestCampaignAdminVersionExhaustionDoesNotWrapOrChangeRule(t *testing.T) {
	db, module := setup(t, nil)
	data := managementRule()
	must(t, module, constants.ActionUpsert, "exhausted", "admin", constants.RoleAdmin, data)
	if err := db.WithContext(context.Background()).Table("activities").Where("id = ?", "exhausted").Update("version", int64(math.MaxInt64)).Error; err != nil {
		t.Fatal(err)
	}
	data["version"] = int64(math.MaxInt64)
	data["name"] = "不得写入的新名称"
	_, err := invoke(module, constants.ActionUpsert, "exhausted", "admin", constants.RoleAdmin, data)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("exhausted version accepted: %v", err)
	}
	var stored struct {
		Name    string
		Version int64
	}
	if err := db.WithContext(context.Background()).Table("activities").Where("id = ?", "exhausted").First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Version != math.MaxInt64 || stored.Name != "运营满减" {
		t.Fatalf("exhausted version mutated: %+v", stored)
	}
}
