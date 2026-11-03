package points_test

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
	"time"
)

func TestMyLedgerPagesBeyondRecentHundredWithoutAcceptingAnotherOwner(t *testing.T) {
	db, m := setup(t, nil)
	rows := []domain.Ledger{}
	for i := 0; i < 125; i++ {
		rows = append(
			rows,
			domain.Ledger{
				ID:        fmt.Sprintf("owned-%03d", i),
				UserID:    "buyer",
				Kind:      "CHECKIN",
				Amount:    20,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
			},
		)
	}
	rows = append(rows, domain.Ledger{ID: "victim", UserID: "victim", Amount: 99999})
	if e := db.Create(&rows).Error; e != nil {
		t.Fatal(e)
	}
	out, e := m.Operations["my_ledger"](
		context.Background(),
		platform.Request{
			ID:     "victim",
			UserID: "buyer",
			Role:   "user",
			Data:   map[string]any{"page": 13, "page_size": 10, "user_id": "victim"},
		},
	)
	if e != nil {
		t.Fatal(e)
	}
	var response struct {
		Items []domain.Ledger
		Total int64
	}
	if e = platform.Decode(out, &response); e != nil {
		t.Fatal(e)
	}
	if response.Total != 125 || len(response.Items) != 5 {
		t.Fatal(out)
	}
	for _, row := range response.Items {
		if row.UserID != "buyer" {
			t.Fatal("owner leaked", row)
		}
	}
	_, e = m.Operations["my_ledger"](context.Background(), platform.Request{})
	if status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
}
