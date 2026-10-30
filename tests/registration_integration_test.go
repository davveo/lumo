package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

func TestPublicRegistrationUsesPolicyAndCreatesOnlyMembers(t *testing.T) {
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	data := map[string]any{"username": "New.Buyer", "password": "Member@2026", "role": "admin", "user_id": "admin"}
	row := h.call("", "iam", "register", "admin", "", data)
	id := platform.Text(row, "user_id")
	if !strings.HasPrefix(id, "USR_") || platform.Text(row, "role") != "user" || platform.Text(row, "username") != "new.buyer" {
		t.Fatal(row)
	}
	h.expect(409, "", "iam", "register", "", "", data)
	h.expect(400, "", "iam", "register", "", "", map[string]any{"username": "abc", "password": "weak"})
	login := h.call("", "iam", "login", "", "", map[string]any{"username": "new.buyer", "password": "Member@2026"})
	principal := h.call(platform.Text(login, "token"), "iam", "current", "", "", nil)
	if platform.Text(principal, "user_id") != id || platform.Text(principal, "role") != "user" {
		t.Fatal(principal)
	}
	var hash string
	if err := c.dbs["iam"].Table("iam_accounts").Select("password_hash").Where("id = ?", id).Scan(&hash).Error; err != nil || hash == "Member@2026" || !strings.HasPrefix(hash, "$2") {
		t.Fatal("password was not hashed", err)
	}
	admin := platform.Text(h.call("", "iam", "login", "", "", map[string]any{"username": "admin", "password": "Admin@2026"}), "token")
	policy := h.call(admin, "iam", "policy_get", "", "", nil)
	config := policy["config"].(map[string]any)
	config["allow_registration"] = false
	h.call(admin, "iam", "policy_upsert", "", "", map[string]any{"config": config, "version": policy["version"], "reason": "测试关闭注册"})
	h.expect(403, "", "iam", "register", "", "", map[string]any{"username": "another.member", "password": "Member@2026"})
	methods := h.call("", "iam", "login_methods", "", "", nil)
	if methods["allow_registration"] != false {
		t.Fatal(methods)
	}
	// Closing registration must not prevent an existing customer from logging in.
	h.call("", "iam", "login", "", "", map[string]any{"username": "new.buyer", "password": "Member@2026"})
}
