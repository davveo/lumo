package order_test

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
	"time"
)

func TestAddressChangeAndWarehouseDispatchShareOneOrderLock(t *testing.T) {
	f := newOrderFixture(t)
	f.create(t, "service-order", "buyer", "", time.Now().Add(time.Hour))
	if e := f.db.Exec("UPDATE orders SET status = ?, resources_committed = ?, version = ? WHERE id = ?", "READY_TO_FULFILL", true, 5, "service-order").Error; e != nil {
		t.Fatal(e)
	}
	address := map[string]any{
		"user_id":   "buyer",
		"recipient": "测试收货人",
		"phone":     "13800000000",
		"region":    "上海市",
		"address":   "测试路1号",
	}
	data := map[string]any{"address": address, "expected_version": 5, "shipping_cents": 0}
	call := func(action, role string, d map[string]any) (platform.Result, error) {
		return f.m.Handle(
			context.Background(),
			platform.Command{Action: action, ID: "service-order", UserID: "buyer", Role: role, Data: d},
		)
	}
	if _, e := call("change_address", "user", data); status.Code(e) != codes.PermissionDenied {
		t.Fatal("public internal snapshot accepted", e)
	}
	if _, e := call("change_address", "system", data); e != nil {
		t.Fatal(e)
	}
	v, e := call("begin_dispatch", "system", nil)
	if e != nil || v["dispatch_started"] != true {
		t.Fatal(v, e)
	}
	data["expected_version"] = platform.Int(v, "version")
	address["region"] = "北京市"
	if _, e = call("change_address", "system", data); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("address changed after dispatch gate", e)
	}
	row := f.row(t, "service-order")
	var saved map[string]any
	_ = json.Unmarshal([]byte(row.Address), &saved)
	if saved["region"] != "上海市" {
		t.Fatal("shipping address changed", saved)
	}
	if _, e = call("urge", "user", nil); e != nil {
		t.Fatal(e)
	}
	if _, e = call("urge", "user", nil); status.Code(e) != codes.ResourceExhausted {
		t.Fatal("urge not rate limited", e)
	}
	if len(f.rpc.commands) != 0 {
		t.Fatal("order transaction made RPC", f.rpc.commands)
	}
}
