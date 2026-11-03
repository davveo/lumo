package httpgateway

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"sync"
	"testing"
)

type searchPeer struct {
	mu    sync.Mutex
	calls []string
}

func (p *searchPeer) Call(_ context.Context, service string, c platform.Command) (platform.Result, error) {
	p.mu.Lock()
	p.calls = append(p.calls, service+"/"+c.Action)
	p.mu.Unlock()
	if service == "logistics" {
		return nil, status.Error(codes.NotFound, "none")
	}
	return platform.Result{
		"items": []platform.Result{
			{"id": "ORD_123", "name": "真实名称", "username": "buyer", "password_hash": "hidden", "receipt": "hidden"},
		},
	}, nil
}
func TestGlobalSearchRestrictsEverySourceAndReturnsOnlyNavigationFields(t *testing.T) {
	for _, role := range []string{"admin", "staff_support", "staff_warehouse", "staff_analyst"} {
		p := &searchPeer{}
		g := gateway{rpc: p}
		out, e := g.managementDashboard(
			context.Background(),
			platform.Command{
				Action: "global_search",
				UserID: "employee",
				Role:   role,
				Data:   map[string]any{"query": "ORD_123"},
			},
		)
		if e != nil {
			t.Fatal(e)
		}
		for _, called := range p.calls {
			part := strings.Split(called, "/")
			if !platform.StaffAllows(role, part[0], part[1]) {
				t.Fatalf("ungranted source %s for %s", called, role)
			}
		}

		var parsed struct {
			Groups []struct {
				Items []map[string]any `json:"items"`
			} `json:"groups"`
		}
		if e = platform.Decode(out, &parsed); e != nil {
			t.Fatal(e)
		}
		for _, group := range parsed.Groups {
			for _, row := range group.Items {
				if row["password_hash"] != nil || row["receipt"] != nil {
					t.Fatal("non-navigation fields leaked", row)
				}
			}
		}
	}
	g := gateway{rpc: &searchPeer{}}
	_, e := g.managementDashboard(
		context.Background(),
		platform.Command{
			Action: "global_search",
			UserID: "buyer",
			Role:   "user",
			Data:   map[string]any{"query": "ORD_123"},
		},
	)
	if status.Code(e) != codes.PermissionDenied {
		t.Fatal(e)
	}
	_, e = g.managementDashboard(
		context.Background(),
		platform.Command{Action: "global_search", UserID: "admin", Role: "admin", Data: map[string]any{"query": "x"}},
	)
	if status.Code(e) != codes.InvalidArgument {
		t.Fatal(e)
	}
}
