package usecase

import (
	"context"
	"errors"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/order/internal/domain"
	"github.com/davveo/lumo/services/order/internal/repository"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type Service struct {
	r   *repository.Repository
	rpc platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{r, rpc} }
func requireUser(actor Actor) error {
	if actor.UserID == "" {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	return nil
}
func own(actor Actor, order domain.Order) error {
	if actor.UserID != order.UserID && actor.Role != "system" && actor.Role != "admin" {
		return status.Error(codes.PermissionDenied, "order belongs to another member")
	}
	return nil
}
func requireInternal(actor Actor) error {
	if actor.Role != "system" && actor.Role != "admin" {
		return status.Error(codes.PermissionDenied, "internal order command")
	}
	return nil
}
func requireInternalUser(actor Actor) error {
	if err := requireUser(actor); err != nil {
		return err
	}
	return requireInternal(actor)
}

type accessPolicy bool

const (
	memberAccess   accessPolicy = false
	internalAccess accessPolicy = true
)

// A transition describes only the persistence and event effects of one use case.
// The individual operation owns its state rules; no action dispatch happens here.
type transition struct {
	unchanged bool
	eventType string
	data      map[string]any
}

func (s *Service) update(ctx context.Context, operation string, request OrderRequest, access accessPolicy, apply func(*repository.Tx, *domain.Order) (transition, error), traceInput ...any) (traceResult domain.Order, traceErr error) {
	ctx, span := platform.Start(ctx, "order.usecase."+operation)
	defer span.End()
	input := any(request)
	if len(traceInput) > 0 {
		input = traceInput[0]
	}
	platform.TraceRequest(ctx, input)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := requireUser(request.Actor); err != nil {
		return domain.Order{}, err
	}
	if access == internalAccess {
		if err := requireInternal(request.Actor); err != nil {
			return domain.Order{}, err
		}
	}
	var result domain.Order
	err := s.r.Transaction(ctx, func(tx *repository.Tx) error {
		order, err := tx.Get(request.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return status.Error(codes.NotFound, "order not found")
		}
		if err != nil {
			return err
		}
		if err = own(request.Actor, order); err != nil {
			return err
		}
		change, err := apply(tx, &order)
		if err != nil {
			return err
		}
		if !change.unchanged {
			if err = tx.Save(&order); err != nil {
				return err
			}
			if change.eventType != "" {
				data := change.data
				if data == nil {
					data = map[string]any{"order_id": order.ID}
				}
				if err = tx.Emit(platform.Event{ID: change.eventType + "-" + order.ID, Type: change.eventType, AggregateID: order.ID, UserID: order.UserID, Data: data}); err != nil {
					return err
				}
			}
		}
		result = order
		return nil
	})
	return result, err
}
