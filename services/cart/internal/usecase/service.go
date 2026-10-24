package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/cart/internal/domain"
	"github.com/davveo/lumo/services/cart/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "cart.usecase.List")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	return s.list(ctx, c, c.UserID, constants.CartRecordKindCartItem, "")
}
func (s *Service) Remove(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "cart.usecase.Remove")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	id := c.ID
	if _, e := s.repo.Get(ctx, id); status.Code(e) == codes.NotFound {
		id = stable(c.UserID, "cart", c.ID)
	}
	if e := s.repo.Delete(ctx, id, c.UserID); e != nil {
		return nil, e
	}
	return platform.Result{"removed": true}, nil
}
func (s *Service) Clear(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "cart.usecase.Clear")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if e := s.repo.DeleteKind(ctx, c.UserID, constants.CartRecordKindCartItem); e != nil {
		return nil, e
	}
	return platform.Result{"cleared": true}, nil
}
func (s *Service) Merge(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "cart.usecase.Merge")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.IdempotencyKey == "" {
		return nil, bad("merge idempotency_key required")
	}
	var input struct {
		Items []struct {
			SKU      string `json:"sku_id"`
			Quantity int64  `json:"quantity"`
		} `json:"items"`
	}
	if e := platform.Decode(c.Data, &input); e != nil {
		return nil, e
	}
	if len(input.Items) > 100 {
		return nil, bad("too many cart lines")
	}
	// Validate all input before writing. A merge uses max quantities, not additive
	// increments, so retries after a partial RPC failure remain idempotent.
	for _, it := range input.Items {
		if it.Quantity < 1 || it.Quantity > 10 {
			return nil, bad("quantity must be 1..10")
		}
		if _, e := platform.NewClients(s.rpc).Catalog.Get(ctx, platform.Request{
			ID:             it.SKU,
			UserID:         c.UserID,
			Role:           c.Role,
			IdempotencyKey: c.IdempotencyKey,
		}); e != nil {
			return nil, e
		}
	}
	for _, it := range input.Items {
		id := stable(c.UserID, "cart", it.SKU)
		seed := record(
			id,
			c.UserID,
			constants.CartRecordKindCartItem,
			constants.CartStatusActive,
			map[string]any{"sku_id": it.SKU, "quantity": it.Quantity},
		)
		if _, e := s.repo.Mutate(ctx, seed, true, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			d := payload(*v)
			if it.Quantity > platform.Int(d, "quantity") {
				d["quantity"] = it.Quantity
				v.Payload = record(v.ID, v.UserID, v.Kind, v.Status, d).Payload
			}
			return nil, nil, nil
		}); e != nil {
			return nil, e
		}
	}
	return s.list(ctx, c, c.UserID, constants.CartRecordKindCartItem, "")
}
func (s *Service) Add(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "cart.usecase.Add")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	return s.changeQuantity(ctx, c, false)
}
func (s *Service) Set(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "cart.usecase.Set")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	return s.changeQuantity(ctx, c, true)
}

func (s *Service) changeQuantity(ctx context.Context, c platform.Request, absolute bool) (platform.Result, error) {
	sku := platform.Text(c.Data, "sku_id")
	quantity := platform.Int(c.Data, "quantity")
	if sku == "" || quantity < 1 || quantity > 10 {
		return nil, bad("sku_id and quantity 1..10 required")
	}
	if _, e := platform.NewClients(s.rpc).Catalog.Get(ctx, platform.Request{
		ID:             sku,
		UserID:         c.UserID,
		Role:           c.Role,
		IdempotencyKey: c.IdempotencyKey,
	}); e != nil {
		return nil, e
	}
	stock, e := platform.NewClients(
		s.rpc,
	).Inventory.Get(
		ctx,
		platform.Request{
			ID:             sku,
			UserID:         c.UserID,
			Role:           c.Role,
			IdempotencyKey: c.IdempotencyKey,
		},
	)
	if e != nil {
		return nil, e
	}
	available := platform.Int(stock, "available")
	id := stable(c.UserID, "cart", sku)
	seed := record(
		id,
		c.UserID,
		constants.CartRecordKindCartItem,
		constants.CartStatusActive,
		map[string]any{"sku_id": sku, "quantity": 0},
	)
	v, e := s.repo.Mutate(ctx, seed, true, func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
		d := payload(*v)
		next := platform.Int(d, "quantity") + quantity
		if absolute {
			next = quantity
		}
		if next > 10 || next > available {
			return nil, nil, status.Error(codes.FailedPrecondition, "cart quantity exceeds per-SKU limit or stock")
		}
		d["quantity"] = next
		v.Payload = record(v.ID, v.UserID, v.Kind, v.Status, d).Payload
		return nil, nil, nil
	})
	if e != nil {
		return nil, e
	}
	return view(v), nil
}
func (s *Service) seed(context.Context) error { return nil }
func (s *Service) event(ctx context.Context, e platform.Event) (traceErr error) {
	if e.Type != constants.EventOrderPaymentAccepted || e.UserID == "" {
		return nil
	}
	ctx, sp := platform.Start(ctx, "cart.usecase.ConsumePaidOrder")
	defer sp.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	o, err := platform.NewClients(
		s.rpc,
	).Order.Get(
		ctx,
		platform.Request{
			ID:     e.AggregateID,
			UserID: e.UserID,
			Role:   constants.RoleSystem,
		},
	)
	if err != nil {
		return err
	}
	if platform.Text(o, "user_id") != e.UserID {
		return status.Error(codes.PermissionDenied, "paid cart event owner mismatch")
	}
	if platform.Text(o, "source") != constants.OrderSourceCart {
		return nil
	}
	b, err := json.Marshal(o["items"])
	if err != nil {
		return err
	}
	var lines []repository.ConsumedLine
	if err = json.Unmarshal(b, &lines); err != nil {
		return err
	}
	return s.repo.ConsumePaidOrder(ctx, e.UserID, e.AggregateID, lines)
}
