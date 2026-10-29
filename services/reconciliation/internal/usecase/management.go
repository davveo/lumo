package usecase

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/reconciliation/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"time"
)

func (s *Service) start(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "reconciliation.usecase.Start")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	from, to := platform.Text(c.Data, "from"), platform.Text(c.Data, "to")
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if reason == "" || len([]rune(reason)) > 240 {
		return nil, status.Error(codes.InvalidArgument, "reconciliation task reason required")
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	a, e := time.ParseInLocation("2006-01-02", from, zone)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "from date required")
	}
	b, e := time.ParseInLocation("2006-01-02", to, zone)
	if e != nil || b.Before(a) || b.Sub(a) > 365*24*time.Hour || c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 {
		return nil, status.Error(codes.InvalidArgument, "valid period up to 366 days and idempotency key required")
	}
	key := sha256.Sum256([]byte(c.UserID + "\x00" + c.IdempotencyKey))
	id, e := s.r.BusinessID(ctx, "recon", c.UserID+"\x00"+c.IdempotencyKey, fmt.Sprintf("recon-%x", key[:16]))
	if e != nil {
		return nil, e
	}
	row := domain.Run{ID: id, From: from, To: to, ActorID: c.UserID, Reason: reason, Status: constants.ReconciliationStatusQueued}
	return s.r.Start(ctx, &row)
}
func (s *Service) AdminList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "reconciliation.usecase.AdminList")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.r.AdminList(ctx, c)
}
func (s *Service) Get(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "reconciliation.usecase.Get")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.r.Get(ctx, c)
}
func (s *Service) Note(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if text := strings.TrimSpace(platform.Text(c.Data, "body")); text == "" || len([]rune(text)) > 2000 || c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 {
		return nil, status.Error(codes.InvalidArgument, "follow-up note of 1..2000 characters required")
	}
	return s.r.Note(ctx, c)
}
func (s *Service) process(ctx context.Context) error {
	ctx, sp := platform.Start(ctx, "reconciliation.usecase.Process")
	defer sp.End()
	run, e := s.r.Claim(ctx)
	if e != nil || run == nil {
		return e
	}
	result, e := platform.NewClients(s.rpc).Payment.AdminList(ctx, platform.Request{UserID: run.ActorID, Role: constants.RoleAdmin, Data: map[string]any{"from": run.From, "to": run.To, "as_of": run.CreatedAt.UTC().Format(time.RFC3339Nano), "page": run.Offset/100 + 1, "page_size": 100}})
	if e != nil {
		return s.r.Progress(ctx, *run, nil, 0, run.Total, false, e)
	}
	var batch struct {
		Items []map[string]any `json:"items"`
		Total int64            `json:"total"`
	}
	if e = platform.Decode(result, &batch); e != nil {
		return s.r.Progress(ctx, *run, nil, 0, run.Total, false, e)
	}
	diffs := []domain.Difference{}
	for _, p := range batch.Items {
		if platform.Text(p, "status") != constants.PaymentStatusSucceeded {
			continue
		}
		order, err := platform.NewClients(s.rpc).Order.Get(ctx, platform.Request{ID: platform.Text(p, "order_id"), UserID: run.ActorID, Role: constants.RoleAdmin})
		kinds := []string{}
		if err != nil {
			if status.Code(err) != codes.NotFound {
				return s.r.Progress(ctx, *run, nil, 0, batch.Total, false, err)
			}
			kinds = append(kinds, constants.ReconciliationDifferenceKindOrderUnavailable)
		} else {
			if platform.Int(order, "payable_cents") != platform.Int(p, "amount_cents") {
				kinds = append(kinds, constants.ReconciliationDifferenceKindAmountMismatch)
			}
			if platform.Text(order, "payment_id") != platform.Text(p, "id") {
				kinds = append(kinds, constants.ReconciliationDifferenceKindPaymentNotAcceptedOrPendingEvent)
			}
		}
		for _, kind := range kinds {
			key := sha256.Sum256([]byte(run.ID + ":" + platform.Text(p, "id") + ":" + kind))
			diffs = append(diffs, domain.Difference{ID: fmt.Sprintf("diff-%x", key[:16]), RunID: run.ID, PaymentID: platform.Text(p, "id"), OrderID: platform.Text(p, "order_id"), Kind: kind})
		}
	}
	processed := int64(len(batch.Items))
	return s.r.Progress(ctx, *run, diffs, processed, batch.Total, processed == 0 || run.Offset+processed >= batch.Total, nil)
}
