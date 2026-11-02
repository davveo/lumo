package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/davveo/lumo/internal/httpgateway"
	"github.com/davveo/lumo/internal/platform"
)

func TestAccountSecurityBindingRecoveryAndSessionOwnership(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	t.Setenv("SMS_WEBHOOK_URL", "")
	t.Setenv("IAM_EMAIL_WEBHOOK_URL", "")
	c := newCluster(t)
	server := httptest.NewServer(httpgateway.NewHandler(c.rpc, true, t.TempDir()))
	defer server.Close()
	h := &httpShop{t: t, server: server, client: server.Client(), coverage: map[string]int{}}
	account := h.call(
		"",
		"iam",
		"register",
		"",
		"",
		map[string]any{"username": "security.buyer", "password": "Member@2026"},
	)
	login := func(password string) string {
		return platform.Text(
			h.call("", "iam", "login", "", "", map[string]any{"username": "security.buyer", "password": password}),
			"token",
		)
	}
	token := login("Member@2026")
	otherToken := platform.Text(
		h.call("", "iam", "login", "", "", map[string]any{"username": "demo", "password": "Lumo@2026"}),
		"token",
	)
	challenge := h.call(
		token,
		"iam",
		"security_send",
		"",
		"",
		map[string]any{"channel": "phone", "address": "13800138013", "current_password": "Member@2026"},
	)
	proof := map[string]any{"challenge_id": challenge["challenge_id"], "code": challenge["demo_code"]}
	h.expect(401, otherToken, "iam", "security_verify", "", "", proof)
	h.call(token, "iam", "security_verify", "", "", proof)
	h.expect(401, token, "iam", "security_verify", "", "", proof)
	security := h.call(token, "iam", "security_get", "", "", nil)
	if security["has_password"] != true || len(security["contacts"].([]any)) != 1 {
		t.Fatal(security)
	}
	sessions := security["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatal(sessions)
	}
	sessionID := platform.Text(sessions[0].(map[string]any), "id")
	h.expect(404, otherToken, "iam", "session_revoke", sessionID, "", nil)
	reset := h.call(
		"",
		"iam",
		"password_reset_request",
		"",
		"",
		map[string]any{"username": "security.buyer", "channel": "phone"},
	)
	h.expect(
		401,
		token,
		"iam",
		"security_verify",
		"",
		"",
		map[string]any{"challenge_id": reset["challenge_id"], "code": reset["demo_code"]},
	)
	resetData := map[string]any{
		"challenge_id": reset["challenge_id"],
		"code":         reset["demo_code"],
		"new_password": "Recovered@2026",
	}
	h.call("", "iam", "password_reset", "", "", resetData)
	h.expect(401, "", "iam", "password_reset", "", "", resetData)
	h.expect(401, token, "iam", "current", "", "", nil)
	token = login("Recovered@2026")
	if platform.Text(h.call(token, "iam", "current", "", "", nil), "user_id") != platform.Text(account, "user_id") {
		t.Fatal("wrong recovered identity")
	}
	h.call(
		token,
		"iam",
		"password_change",
		"",
		"",
		map[string]any{"current_password": "Recovered@2026", "new_password": "Changed@2026"},
	)
	h.expect(401, token, "iam", "current", "", "", nil)
	token = login("Changed@2026")
	security = h.call(token, "iam", "security_get", "", "", nil)
	sessions = security["sessions"].([]any)
	h.call(token, "iam", "session_revoke", platform.Text(sessions[0].(map[string]any), "id"), "", nil)
	h.expect(401, token, "iam", "current", "", "", nil)
	// Recovery responses do not reveal whether an account has a bound contact.
	unknown := h.call(
		"",
		"iam",
		"password_reset_request",
		"",
		"",
		map[string]any{"username": "missing.buyer", "channel": "email"},
	)
	if len(platform.Text(unknown, "challenge_id")) != 64 || unknown["demo_code"] != nil {
		t.Fatal(unknown)
	}
}
