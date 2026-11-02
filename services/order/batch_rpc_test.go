package order_test

import (
	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
	"time"
)

func TestBatchGetOrdersTypedRPCRequiresInternalActor(t *testing.T) {
	f := newOrderRPCFixture(t)
	f.create(t, "batch-a", "buyer-a", "", time.Now().Add(time.Hour))
	f.create(t, "batch-b", "buyer-b", "", time.Now().Add(time.Hour))
	req := &mallv1.BatchGetOrdersRequest{
		Actor: orderActor("worker", "system"),
		Ids:   []string{"batch-b", "missing", "batch-a", "batch-a"},
	}
	got, e := f.client.BatchGetOrders(f.ctx, req)
	if e != nil || len(got.GetItems()) != 2 || got.Items[0].GetId() != "batch-a" || got.Items[1].GetId() != "batch-b" {
		t.Fatalf("batch missing/dedup/order: %v %v", got, e)
	}
	req.Actor = orderActor("buyer-a", "user")
	if _, e = f.client.BatchGetOrders(f.ctx, req); status.Code(e) != codes.PermissionDenied {
		t.Fatal("private orders leaked", e)
	}
	req.Actor = orderActor("", "system")
	if _, e = f.client.BatchGetOrders(f.ctx, req); status.Code(e) != codes.Unauthenticated {
		t.Fatal(e)
	}
	req.Actor = orderActor("worker", "system")
	req.Ids = []string{" batch-a"}
	if _, e = f.client.BatchGetOrders(f.ctx, req); status.Code(e) != codes.InvalidArgument {
		t.Fatal(e)
	}
}
