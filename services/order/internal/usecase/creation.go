package usecase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"io"
	"time"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

func decodeQuote(snapshot json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(snapshot))
	decoder.UseNumber()
	var quote map[string]any
	if err := decoder.Decode(&quote); err != nil || quote == nil {
		return nil, status.Error(codes.InvalidArgument, "quote snapshot required")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, status.Error(codes.InvalidArgument, "quote snapshot required")
	}
	return quote, nil
}
func (s *Service) Create(ctx context.Context, request CreateOrderRequest) (traceResult domain.Order, traceErr error) {
	ctx, span := platform.Start(ctx, "order.usecase.create")
	defer span.End()
	platform.TraceRequest(ctx, request)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := requireInternalUser(request.Actor); err != nil {
		return domain.Order{}, err
	}
	var result domain.Order
	err := s.r.Transaction(ctx, func(tx *repository.Tx) error {
		order, err := tx.Get(request.ID)
		if err == nil {
			if err = own(request.Actor, order); err != nil {
				return err
			}
			result = order
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if request.ID == "" {
			return status.Error(codes.InvalidArgument, "order ID required")
		}
		quote, err := decodeQuote(request.Quote)
		if err != nil {
			return err
		}
		amount := platform.Int(quote, "payable_cents")
		if amount <= 0 {
			return status.Error(codes.InvalidArgument, "payable amount required")
		}
		address := request.Address
		if len(address) == 0 {
			address = json.RawMessage("null")
		} else if !json.Valid(address) {
			return status.Error(codes.InvalidArgument, "invalid address snapshot")
		}
		expiresAt := request.ExpiresAt
		if expiresAt.IsZero() {
			expiresAt = time.Now().UTC().Add(30 * time.Minute)
		}
		order = domain.Order{
			ID:        request.ID,
			UserID:    request.Actor.UserID,
			Status:    constants.OrderStatusCreating,
			Version:   1,
			Payable:   amount,
			Quote:     string(request.Quote),
			Address:   string(address),
			Note:      request.Note,
			Source:    request.Source,
			GroupID:   platform.Text(quote, "group_id"),
			ExpiresAt: expiresAt,
		}
		if err = tx.Create(&order); err != nil {
			return err
		}
		result = order
		return nil
	})
	return result, err
}
func (s *Service) Pending(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(
		ctx,
		constants.ActionPending,
		request,
		internalAccess,
		func(_ *repository.Tx, order *domain.Order) (transition, error) {
			if order.Status == constants.OrderStatusPendingPayment {
				return transition{unchanged: true}, nil
			}
			if order.Status != constants.OrderStatusCreating {
				return transition{}, status.Error(codes.FailedPrecondition, "order cannot become payable")
			}
			order.Status = constants.OrderStatusPendingPayment
			return transition{eventType: constants.EventOrderAwaitingPayment}, nil
		},
	)
}
func (s *Service) CreateFailed(ctx context.Context, request OrderRequest) (domain.Order, error) {
	return s.update(
		ctx,
		constants.ActionCreateFailed,
		request,
		internalAccess,
		func(_ *repository.Tx, order *domain.Order) (transition, error) {
			if order.Status == constants.OrderStatusCreating {
				order.Status = constants.OrderStatusCreateFailed
			}
			return transition{}, nil
		},
	)
}
