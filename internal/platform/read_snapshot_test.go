package platform

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestSnapshotPreflightsLocalReadInventoryAndPropagatesCancellation(t *testing.T) {
	calls := 0
	m := Module{
		Name: constants.ServiceCatalog,
		Operations: map[string]Operation{
			constants.ActionGet: func(_ context.Context, c Request) (Result, error) { calls++; return Result{"id": c.ID}, nil },
		},
	}
	cmd := Command{
		Data: map[string]any{
			"requests": []Command{{Action: constants.ActionGet, ID: "a"}, {Action: constants.ActionUpsert, ID: "b"}},
		},
	}
	if _, e := ApplyReadSnapshot(context.Background(), m, cmd); status.Code(e) != codes.PermissionDenied || calls != 0 {
		t.Fatalf("unauthorized snapshot executed reads: %d %v", calls, e)
	}
	cmd.Data["requests"] = []Command{{Action: constants.ActionGet, ID: "a"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, e := ApplyReadSnapshot(ctx, m, cmd)
	var rows struct {
		Results []Result `json:"results"`
	}
	_ = Decode(got, &rows)
	if e != nil || calls != 0 || len(rows.Results) != 1 ||
		Int(Map(rows.Results[0]["error"]), "code") != int64(codes.Canceled) {
		t.Fatalf("cancellation not propagated: %v %v", got, e)
	}
}
