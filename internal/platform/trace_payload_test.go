package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func captureTracePayload(
	t *testing.T,
	request, response any,
	err error,
) (tracetest.SpanStub, map[string]attribute.Value) {
	t.Helper()
	exporter, _ := tracing(t)
	ctx, span := Start(context.Background(), "test.payload")
	TraceRequest(ctx, request)
	TraceResponse(ctx, response, err)
	span.End()
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d payload spans", len(spans))
	}
	attrs := map[string]attribute.Value{}
	for _, value := range spans[0].Attributes {
		attrs[string(value.Key)] = value.Value
	}
	return spans[0], attrs
}

func TestTracePayloadRedactsPrivateFieldsWithoutChangingBusinessValues(t *testing.T) {
	data := map[string]any{
		"sku_id":       "sku-test",
		"quantity":     2,
		"amount_cents": json.Number("9223372036854775807"),
		"password":     "private-password",
		"code":         "private-otp-code", "demo_code": "private-demo-code", "proof": "private-browser-proof", "oauth_state": "private-oauth-state", "challenge_id": "private-challenge-id",
		"accessToken":   "private-token",
		"phone":         "13521373352",
		"user_id":       "private-user",
		"username":      "private-username",
		"address":       map[string]any{"street": "private-street"},
		"upload_fields": map[string]any{"key": "private-object", "policy": "private-policy"},
		"download_url":  "https://storage.test/private?signature=private-signature",
		"nested": map[string]any{
			"Authorization": "Bearer private-authorization",
			"new_password":  "private-new-password",
			"body":          "private-message",
		},
	}
	request := Command{
		Action:         "create",
		ID:             "business-order",
		UserID:         "private-user",
		IdempotencyKey: "private-retry-key",
		Data:           data,
	}
	_, attrs := captureTracePayload(
		t,
		request,
		map[string]any{
			"id":     "business-order",
			"status": "PENDING",
			"items":  []any{map[string]any{"name": "商品名称", "sku_id": "sku-test"}},
		},
		nil,
	)
	encoded := attrs["lumo.request"].AsString()
	for _, value := range []string{
		"private-otp-code", "private-demo-code", "private-browser-proof", "private-oauth-state", "private-challenge-id",
		"private-password",
		"private-token",
		"13521373352",
		"private-user",
		"private-username",
		"private-street",
		"private-object",
		"private-policy",
		"private-signature",
		"private-authorization",
		"private-new-password",
		"private-message",
		"private-retry-key",
	} {
		if strings.Contains(encoded, value) {
			t.Errorf("private value %q entered Trace", value)
		}
	}
	for _, value := range []string{"business-order", "sku-test", "9223372036854775807"} {
		if !strings.Contains(encoded, value) {
			t.Errorf("business value %q missing from request preview: %s", value, encoded)
		}
	}
	if !attrs["lumo.request.redacted"].AsBool() || attrs["lumo.request.truncated"].AsBool() {
		t.Fatalf("unexpected preview flags: %v", attrs)
	}
	if !strings.Contains(attrs["lumo.response"].AsString(), "商品名称") || attrs["lumo.error.code"].AsString() != "OK" {
		t.Fatal("response or success code not recorded")
	}
	if data["phone"] != "13521373352" || data["password"] != "private-password" || request.UserID != "private-user" {
		t.Fatal("tracing mutated business input")
	}
}

func TestTracePayloadSafelyExpandsProtobufAndNestedJSONDocuments(t *testing.T) {
	raw := []byte(
		`{"sku_id":"sku-proto","amount_cents":9223372036854775807,"password":"private-json-secret","nested":"{\"token\":\"private-encoded-token\",\"quantity\":3}"}`,
	)
	request := &mallv1.Command{Action: "create", Id: "business-order", UserId: "private-user", PayloadJson: raw}
	quote := []byte(`{"payable_cents":9223372036854775807,"items":[{"sku_id":"sku-proto","phone":"13521373352"}]}`)
	order := &mallv1.Order{
		Id:                  "business-order",
		UserId:              "private-user",
		PayableCents:        9223372036854775807,
		QuoteSnapshotJson:   quote,
		AddressSnapshotJson: []byte(`{"recipient":"private-recipient","address":"private-address"}`),
	}
	_, attrs := captureTracePayload(t, request, order, nil)
	for _, key := range []string{"lumo.request", "lumo.response"} {
		preview := attrs[key].AsString()
		if !json.Valid([]byte(preview)) || !strings.Contains(preview, "9223372036854775807") ||
			!strings.Contains(preview, "sku-proto") {
			t.Errorf("missing exact structured protobuf values: %s", preview)
		}
		for _, private := range []string{
			"private-user",
			"private-json-secret",
			"private-encoded-token",
			"13521373352",
			"private-recipient",
			"private-address",
		} {
			if strings.Contains(preview, private) {
				t.Errorf("private protobuf/embedded JSON value %q entered Trace: %s", private, preview)
			}
		}
	}
	if !strings.Contains(attrs["lumo.request"].AsString(), `"quantity":3`) || !bytes.Equal(raw, request.PayloadJson) ||
		!bytes.Equal(quote, order.QuoteSnapshotJson) {
		t.Fatal("nested JSON was not expanded safely or source bytes changed")
	}
}

func TestTracePayloadBoundsHugeResultsAndKeepsValidUTF8JSON(t *testing.T) {
	large := make([]any, 100000)
	item := map[string]any{}
	for i := 0; i < 60; i++ {
		item[fmt.Sprintf("field_%02d", i)] = strings.Repeat("测😀试", 100)
	}
	for i := range large {
		large[i] = item
	}
	_, attrs := captureTracePayload(
		t,
		map[string]any{"items": large},
		&mallv1.Result{
			PayloadJson: []byte(
				`{"items":[` + strings.Repeat(`{"id":"business-item"},`, 9999) + `{"id":"last-item"}]}`,
			),
		},
		nil,
	)
	for _, key := range []string{"lumo.request", "lumo.response"} {
		preview := attrs[key].AsString()
		if len(preview) > tracePayloadMaxBytes || !utf8.ValidString(preview) || !json.Valid([]byte(preview)) ||
			!attrs[key+".truncated"].AsBool() {
			t.Errorf("unbounded or invalid preview for %s: bytes=%d", key, len(preview))
		}
	}
	if len(large) != 100000 || len(item) != 60 ||
		!strings.Contains(attrs["lumo.response"].AsString(), "business-item") ||
		strings.Contains(attrs["lumo.response"].AsString(), "last-item") {
		t.Fatal("preview changed business data or failed to bound the list")
	}
}

func TestTracePayloadErrorsNeverExportOriginalErrorText(t *testing.T) {
	for _, test := range []struct {
		err  error
		code string
	}{
		{status.Error(codes.InvalidArgument, "private-error-secret"), "InvalidArgument"},
		{fmt.Errorf("database connection password=private-error-secret"), "Internal"},
		{fmt.Errorf("wrapped: %w", context.DeadlineExceeded), "DeadlineExceeded"},
		{context.Canceled, "Canceled"},
	} {
		t.Run(test.code, func(t *testing.T) {
			span, attrs := captureTracePayload(t, map[string]any{"id": "business-order"}, nil, test.err)
			if attrs["lumo.error.code"].AsString() != test.code ||
				!strings.Contains(attrs["lumo.response"].AsString(), test.code) ||
				span.Status.Description != test.code {
				t.Fatalf("missing safe error result: %+v", span)
			}
			encoded, _ := json.Marshal(span)
			if strings.Contains(string(encoded), "private-error-secret") {
				t.Fatal("original error text entered Trace")
			}
		})
	}
}

type unsafeTraceMarshaler struct{ ID string }

func (unsafeTraceMarshaler) MarshalJSON() ([]byte, error) {
	panic("business marshaler must never run for Trace")
}

func TestTracePayloadHandlesMalformedDocumentsCyclesAndUnsupportedValues(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	var pointerCycle any
	pointerCycle = &pointerCycle
	for _, request := range []any{
		cycle, pointerCycle, json.RawMessage(`{"password":"private-broken-document",`), make(chan int),
		unsafeTraceMarshaler{ID: "business-safe"}, []byte("private-binary-content"),
		json.RawMessage(`{"sku_id":"safe"} {"token":"private-trailing-token"}`),
	} {
		t.Run(fmt.Sprintf("%T", request), func(t *testing.T) {
			_, attrs := captureTracePayload(t, request, nil, nil)
			preview := attrs["lumo.request"].AsString()
			if !json.Valid([]byte(preview)) || len(preview) > tracePayloadMaxBytes ||
				strings.Contains(preview, "private-") {
				t.Fatalf("unsafe diagnostic output: %s", preview)
			}
		})
	}
}

func TestTracePayloadRemovesSecretsFromGenericStringsAndJSONText(t *testing.T) {
	quoted, _ := json.Marshal(`{"token":"private-quoted-token","quantity":4}`)
	request := map[string]any{
		"query":           "send to private@example.test at 13521373352",
		"name":            "Bearer private-embedded-token",
		"image_url":       "https://image.test/path?X-Amz-Signature=private-signature",
		"wrapped":         `{"inner":"{\"access_token\":\"private-double-json\",\"count\":2}"}`,
		"quoted":          string(quoted),
		"label":           "查看 https://storage.test/file?X-Amz-Signature=private-embedded-signature",
		"label_with_json": `prefix {"token":"private-prefixed-token"}`,
		"assignment":      "password=private-password-assignment",
		"用户名":             "private-Chinese-username",
		"order_id":        "ord_13521373352123",
	}
	_, attrs := captureTracePayload(t, request, nil, errors.New("private-error"))
	preview := attrs["lumo.request"].AsString()
	for _, private := range []string{
		"private@example.test",
		"private-embedded-token",
		"private-signature",
		"private-double-json",
		"private-quoted-token",
		"private-embedded-signature",
		"private-prefixed-token",
		"private-password-assignment",
		"private-Chinese-username",
	} {
		if strings.Contains(preview, private) {
			t.Fatalf("embedded private value leaked: %s", preview)
		}
	}
	if !strings.Contains(preview, `"count":2`) || !strings.Contains(preview, `"quantity":4`) {
		t.Fatalf("encoded JSON business value lost: %s", preview)
	}
	if !strings.Contains(preview, "ord_13521373352123") || strings.Contains(preview, "at 13521373352") {
		t.Fatalf("phone redaction damaged a business ID or retained a standalone phone: %s", preview)
	}
}

func TestTracePayloadBoundsRepeatedSensitiveJSONFields(t *testing.T) {
	raw := json.RawMessage(
		`{` + strings.Repeat(`"password":"private-repeated-secret",`, 1000) + `"id":"should-not-be-visited"}`,
	)
	_, attrs := captureTracePayload(t, raw, nil, nil)
	preview := attrs["lumo.request"].AsString()
	if !attrs["lumo.request.truncated"].AsBool() || !attrs["lumo.request.redacted"].AsBool() ||
		!json.Valid([]byte(preview)) ||
		strings.Contains(preview, "private-repeated-secret") ||
		strings.Contains(preview, "should-not-be-visited") {
		t.Fatalf("repeated sensitive keys bypassed bounded redaction: %s", preview)
	}
}

func TestTracePayloadRedactsMarketingOwnershipAndAudienceAcrossBoundaries(t *testing.T) {
	data := map[string]any{
		"campaign_id": "welcome-campaign",
		"placement":   "home",
		"visitor_id":  "private-popup-visitor",
		"delivery_id": "private-popup-delivery",
		"view_id":     "private-popup-view",
		"session_id":  "private-popup-session",
		"audience": map[string]any{
			"segments":         []string{"new_member"},
			"include_user_ids": []string{"private-allowed-a", "private-allowed-b"},
			"exclude_user_ids": []any{"private-excluded-a", "private-excluded-b"},
		},
		// Key normalization must cover camelCase and separators in embedded data.
		"nested": map[string]any{
			"visitorId":   "private-camel-visitor",
			"Delivery-ID": "private-separated-delivery",
			"viewId":      "private-camel-view",
			"rules": []any{
				map[string]any{
					"includeUserIDs":   []string{"private-camel-allowed"},
					"exclude-user-ids": []string{"private-separated-excluded"},
				},
			},
		},
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), raw...)
	command := Command{Action: "popup_event", Data: data}
	protoCommand := &mallv1.Command{Action: "popup_event", PayloadJson: raw}
	protoResult := &mallv1.Result{PayloadJson: raw}
	for _, test := range []struct {
		name              string
		request, response any
	}{
		{"http-usecase-command", command, map[string]any{"popup": data}},
		{"rpc-json-bytes", protoCommand, protoResult},
		{"embedded-document", map[string]any{"payload": string(raw)}, json.RawMessage(raw)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, attrs := captureTracePayload(t, test.request, test.response, nil)
			for _, key := range []string{"lumo.request", "lumo.response"} {
				preview := attrs[key].AsString()
				if !json.Valid([]byte(preview)) || strings.Contains(preview, "private-") ||
					!attrs[key+".redacted"].AsBool() {
					t.Fatalf("marketing identity or audience principal exported by %s: %s", key, preview)
				}
				for _, business := range []string{"welcome-campaign", "home", "new_member"} {
					if !strings.Contains(preview, business) {
						t.Errorf("marketing business metadata %q missing from %s: %s", business, key, preview)
					}
				}
			}
		})
	}
	after, err := json.Marshal(data)
	if err != nil || !bytes.Equal(before, after) || !bytes.Equal(raw, protoCommand.PayloadJson) ||
		!bytes.Equal(raw, protoResult.PayloadJson) {
		t.Fatal("marketing trace redaction mutated business commands or RPC bytes")
	}
}
