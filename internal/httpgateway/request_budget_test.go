package httpgateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
)

type budgetBody struct {
	io.Reader
	onRead func()
}

func (b *budgetBody) Read(p []byte) (int, error) {
	if b.onRead != nil {
		f := b.onRead
		b.onRead = nil
		f()
	}
	return b.Reader.Read(p)
}

func (b *budgetBody) Close() error { return nil }

func TestGatewayRequestBudgetIncludesBodyAndAuthentication(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		name := "public"
		if authenticated {
			name = "authenticated"
		}
		t.Run(name, func(t *testing.T) {
			var upperBound, forwardedDeadline time.Time
			caller := traceTestCaller(
				func(ctx context.Context, service string, _ platform.Command) (platform.Result, error) {
					if service == "iam" {
						// Session validation consumes the same overall HTTP budget.
						time.Sleep(10 * time.Millisecond)
						return platform.Result{"user_id": "buyer", "role": "user"}, nil
					}
					var ok bool
					forwardedDeadline, ok = ctx.Deadline()
					if !ok {
						t.Fatal("business RPC has no deadline")
					}
					return platform.Result{"items": []any{}}, nil
				},
			)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/list", nil)
			request.Body = &budgetBody{Reader: strings.NewReader(`{"data":{}}`), onRead: func() {
				upperBound = time.Now().Add(25 * time.Second)
				// Model time spent receiving the request body before dispatch.
				time.Sleep(10 * time.Millisecond)
			}}
			if authenticated {
				request.Header.Set("Authorization", "Bearer session")
			}
			response := httptest.NewRecorder()
			NewHandler(caller, false, "").ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d: %s", response.Code, response.Body)
			}
			if forwardedDeadline.IsZero() || forwardedDeadline.After(upperBound) {
				t.Fatalf(
					"body/auth time incorrectly resets the RPC budget: deadline=%v upperBound=%v",
					forwardedDeadline,
					upperBound,
				)
			}
		})
	}
}
