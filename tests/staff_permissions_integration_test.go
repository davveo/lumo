package tests

import (
	"context"
	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http/httptest"
	"testing"
)

func TestStaffRolesEnforceHTTPAndNamedRPCPermissions(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	login := func(name, pwd string) platform.Result {
		return h.call("", "iam", "login", "", "", map[string]any{"username": name, "password": pwd})
	}
	admin := platform.Text(login("admin", "Admin@2026"), "token")
	employee := h.call(
		admin,
		"iam",
		"staff_upsert",
		"",
		"",
		map[string]any{
			"username": "warehouse.worker",
			"password": "Worker@2026",
			"role":     "staff_warehouse",
			"active":   true,
			"reason":   "仓库岗位",
		},
	)
	session := login("warehouse.worker", "Worker@2026")
	token := platform.Text(session, "token")
	if session["staff"] != true {
		t.Fatal(session)
	}
	h.call(token, "inventory", "admin_list", "", "", nil)
	h.expect(403, token, "iam", "staff_list", "", "", nil)
	h.expect(403, token, "payment", "admin_list", "", "", nil)
	h.expect(403, token, "catalog", "upsert", "blocked-product", "", map[string]any{"name": "not allowed"})
	// Bypassing HTTP cannot turn a warehouse employee into a finance operator.
	_, err := c.rpc.Call(
		context.Background(),
		"refund",
		platform.Command{
			Action: "confirm_demo",
			UserID: platform.Text(employee, "id"),
			Role:   "staff_warehouse",
			ID:     "none",
		},
	)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatal("RPC role boundary bypass", err)
	}
	h.call(
		admin,
		"iam",
		"staff_upsert",
		platform.Text(employee, "id"),
		"",
		map[string]any{
			"role":          "staff_finance",
			"expected_role": "staff_warehouse",
			"active":        true,
			"reason":        "调岗",
		},
	)
	h.expect(401, token, "iam", "current", "", "", nil)
	token = platform.Text(login("warehouse.worker", "Worker@2026"), "token")
	h.call(token, "payment", "admin_list", "", "", nil)
	h.expect(403, token, "inventory", "adjust", "sku-coffee", "", map[string]any{"on_hand": 999, "reason": "blocked"})
}
