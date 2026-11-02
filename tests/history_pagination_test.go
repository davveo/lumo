package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHistoryPaginationAcrossRPCAndOwnership(t *testing.T) {
	c := newCluster(t)
	ctx := context.Background()
	for _, service := range []string{"order", "payment", "aftersale", "invoice"} {
		t.Run(service, func(t *testing.T) {
			table := map[string]string{"order": "orders", "payment": "payments", "aftersale": "records", "invoice": "records"}[service]
			created := time.Now().UTC().Truncate(time.Second)
			for i := 0; i < 105; i++ {
				row := map[string]any{
					"id":         fmt.Sprintf("history-%03d", i),
					"user_id":    "demo",
					"created_at": created,
					"updated_at": created,
				}
				if service != "order" {
					row["order_id"] = fmt.Sprintf("order-%03d", i)
				}
				if err := c.dbs[service].Table(table).Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			role := "user"
			if service != "payment" {
				victim := map[string]any{
					"id":         "history-050-victim",
					"user_id":    "another-user",
					"created_at": created,
					"updated_at": created,
				}
				if service != "order" {
					victim["order_id"] = "another-users-order"
				}
				if err := c.dbs[service].Table(table).Create(victim).Error; err != nil {
					t.Fatal(err)
				}
			}
			if service == "payment" {
				role = "admin"
			}
			seen := map[string]bool{}
			cursor := ""
			for page := 0; page < 3; page++ {
				result, err := c.rpc.Call(
					ctx,
					service,
					platform.Command{
						Action: "list",
						UserID: "demo",
						Role:   role,
						Data:   map[string]any{"limit": 50, "cursor": cursor},
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				if platform.Int(result, "total") != 105 {
					t.Fatal("wrong total", result)
				}
				for _, row := range mapItems(result) {
					if platform.Text(row, "user_id") != "demo" {
						t.Fatal("another user's record leaked into a page")
					}
					id := platform.Text(row, "id")
					if seen[id] {
						t.Fatal("duplicate ID across tied timestamps", id)
					}
					seen[id] = true
				}
				if page == 0 && service != "payment" {
					_, err = c.rpc.Call(
						ctx,
						service,
						platform.Command{
							Action: "list",
							UserID: "another-user",
							Role:   "user",
							Data:   map[string]any{"cursor": result["next_cursor"]},
						},
					)
					if status.Code(err) != codes.InvalidArgument {
						t.Fatal("cursor crossed account scope", err)
					}
				}
				cursor = platform.Text(result, "next_cursor")
				if page < 2 && cursor == "" {
					t.Fatal("history truncated")
				}
				if page == 2 && (cursor != "" || result["has_more"] != false) {
					t.Fatal("last page did not end", result)
				}
			}
			if len(seen) != 105 {
				t.Fatal("older records missing", len(seen))
			}
		})
	}
}
