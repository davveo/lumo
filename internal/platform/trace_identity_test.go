package platform

import (
	"encoding/json"
	"strings"
	"testing"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
)

func TestTracePayloadRedactsPlainIDsThatAliasPrivateIdentityFields(t *testing.T) {
	type profile struct {
		ID     string `json:"id"`
		UserID string `json:"user_id"`
		Kind   string `json:"kind"`
	}
	quotedIdentity, err := json.Marshal(`{"id":"private-principal","user_id":"private-principal","kind":"profile"}`)
	if err != nil {
		t.Fatal(err)
	}
	wrappedIdentity, err := json.Marshal(map[string]any{"wrapped": string(quotedIdentity)})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		input any
	}{
		{
			"map",
			map[string]any{
				"id":      "private-principal",
				"user_id": "private-principal",
				"kind":    "profile",
				"sku_id":  "sku-public",
			},
		},
		{"struct", profile{ID: "private-principal", UserID: "private-principal", Kind: "profile"}},
		{"protobuf", &mallv1.Order{Id: "private-principal", UserId: "private-principal", Status: "PENDING"}},
		{"json", json.RawMessage(`{"id":"private-principal","user_id":"private-principal","kind":"profile"}`)},
		{
			"protobuf-json",
			&mallv1.Result{
				PayloadJson: []byte(
					`{"items":[{"id":"private-principal","user_id":"private-principal","kind":"profile"}]}`,
				),
			},
		},
		{
			"json-string",
			map[string]any{"payload": `{"id":"private-principal","user_id":"private-principal","kind":"profile"}`},
		},
		{"quoted-json-string", map[string]any{"wrapped": string(quotedIdentity)}},
		{"protobuf-quoted-json", &mallv1.Result{PayloadJson: wrappedIdentity}},
		{
			"nested-alias",
			map[string]any{
				"items": []any{map[string]any{"id": "private-principal"}},
				"actor": map[string]any{"username": "private-principal"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, err := json.Marshal(test.input)
			if err != nil {
				t.Fatal(err)
			}
			_, attrs := captureTracePayload(t, test.input, test.input, nil)
			for _, key := range []string{"lumo.request", "lumo.response"} {
				preview := attrs[key].AsString()
				if strings.Contains(preview, "private-principal") || !attrs[key+".redacted"].AsBool() {
					t.Errorf("private identity alias exported: %s", preview)
				}
			}
			after, err := json.Marshal(test.input)
			if err != nil || string(before) != string(after) {
				t.Fatal("identity redaction changed business data")
			}
		})
	}
}

func TestTracePayloadPreservesUnrelatedBusinessIDsAndDoesNotRememberCredentials(t *testing.T) {
	for _, key := range []string{"user_id", "uid", "username", "owner_id", "member_id", "account_id", "customer_id"} {
		t.Run(key, func(t *testing.T) {
			request := map[string]any{
				key:     "private-principal",
				"id":    "order-public",
				"items": []any{map[string]any{"id": "sku-public"}},
			}
			_, attrs := captureTracePayload(t, request, nil, nil)
			preview := attrs["lumo.request"].AsString()
			if strings.Contains(preview, "private-principal") || !strings.Contains(preview, "order-public") ||
				!strings.Contains(preview, "sku-public") {
				t.Fatalf("unrelated business identifiers were lost: %s", preview)
			}
		})
	}
	preview := &payloadPreview{}
	preview.rememberIdentity("password", "credential-value")
	preview.rememberIdentity("token", "credential-value")
	preview.rememberIdentity("username", strings.Repeat("x", tracePayloadMaxString+1))
	if len(preview.identities) != 0 {
		t.Fatal("credentials or oversized identity copied into alias storage")
	}
}
