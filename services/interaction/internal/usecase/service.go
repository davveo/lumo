package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/interaction/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "interaction.usecase.list")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.InteractionKindReview
	}
	if kind != constants.InteractionKindReview && kind != constants.InteractionKindQuestion {
		return nil, bad("kind must be review or question")
	}
	owner := ""
	st := constants.InteractionStatusPublished
	if pending, _ := c.Data["include_pending"].(bool); pending {
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		st = ""
	}
	if mine, _ := c.Data["mine"].(bool); mine {
		if e := platform.RequireUser(c); e != nil {
			return nil, e
		}
		owner = c.UserID
		st = ""
	}
	data, e := s.list(ctx, c, owner, kind, st)
	if e != nil {
		return nil, e
	}
	return data, nil
}
func (s *Service) Create(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "interaction.usecase.create")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.InteractionKindReview
	}
	sku := platform.Text(c.Data, "sku_id")
	body := platform.Text(c.Data, "body")
	if sku == "" || body == "" || len(body) > 4000 {
		return nil, bad("sku_id and body required")
	}

	if kind == constants.InteractionKindReview {
		rating := platform.Int(c.Data, "rating")
		if rating < 1 || rating > 5 {
			return nil, bad("rating must be 1..5")
		}
		oid := platform.Text(c.Data, "order_id")
		o, e := platform.NewClients(
			s.rpc,
		).Order.Get(
			ctx,
			platform.Request{ID: oid, UserID: c.UserID, Role: c.Role, IdempotencyKey: c.IdempotencyKey, Data: nil},
		)
		if e != nil {
			return nil, e
		}
		if platform.Text(o, "user_id") != c.UserID || platform.Text(o, "status") != constants.OrderStatusCompleted {
			return nil, status.Error(codes.FailedPrecondition, "completed owned order required")
		}
		var snap struct {
			Items []struct {
				SKU string `json:"sku_id"`
			} `json:"items"`
		}
		if e = platform.Decode(o, &snap); e != nil {
			return nil, e
		}
		found := false
		for _, it := range snap.Items {
			if it.SKU == sku {
				found = true
			}
		}
		if !found {
			return nil, bad("SKU not purchased in order")
		}
  var fulfillment struct{Lines []struct{SKU string `json:"sku_id"`;Quantity int64 `json:"quantity"`;Cancelled int64 `json:"cancelled"`} `json:"fulfillment_progress"`}
  _=platform.Decode(o,&fulfillment)
  for _,l:=range fulfillment.Lines{if l.SKU==sku&&l.Quantity<=l.Cancelled{return nil,status.Error(codes.FailedPrecondition,"已取消且未收货的商品不能评价")}}
		c.ID = stable(c.UserID, constants.InteractionKindReview, oid, sku)
	} else if kind == constants.InteractionKindQuestion {
		if _, e := platform.NewClients(s.rpc).Catalog.Get(ctx, platform.Request{ID: sku, UserID: c.UserID, Role: c.Role}); e != nil {
			return nil, e
		}
		if c.IdempotencyKey == "" {
			return nil, bad("question idempotency_key required")
		}
		c.ID = stable(c.UserID, constants.InteractionKindQuestion, c.IdempotencyKey)
	} else {
		return nil, bad("kind must be review or question")
	}
	ids, e := photoIDs(c.Data)
	if e != nil {
		return nil, e
	}
	if kind == constants.InteractionKindQuestion && len(ids) > 0 {
		return nil, bad("提问暂不支持图片附件")
	}
	if kind == constants.InteractionKindReview {
		if e = s.verifyReviewPhotos(ctx, c.UserID, ids); e != nil {
			return nil, e
		}
	}
	var idErr error
	c.ID, idErr = s.repo.BusinessID(ctx, kind, c.ID, c.ID)
	if idErr != nil {
		return nil, idErr
	}
	r := record(
		c.ID,
		c.UserID,
		kind,
		constants.InteractionStatusPending,
		map[string]any{
			"sku_id":    sku,
			"body":      body,
			"rating":    platform.Int(c.Data, "rating"),
			"order_id":  platform.Text(c.Data, "order_id"),
			"photo_ids": ids,
		},
	)
	r.ParentID = sku
	inserted, e := s.repo.InsertOnce(ctx, &r, nil)
	if e != nil {
		return nil, e
	}
	if !inserted {
		r, e = s.repo.Get(ctx, r.ID)
		if e != nil {
			return nil, e
		}
	}
	return view(r), nil
}
func (s *Service) Moderate(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "interaction.usecase.moderate")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.moderateContent(ctx, c)
}

func (s *Service) Answer(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "interaction.usecase.answer")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	r, e := s.repo.Mutate(
		ctx,
		domain.Record{ID: c.ID},
		false,
		func(v *domain.Record) ([]domain.Record, *platform.Event, error) {
			if v.Kind != constants.InteractionKindQuestion {
				return nil, nil, status.Error(codes.FailedPrecondition, "只能回复商品提问，评价请使用审核操作")
			}
			if _, exists := c.Data["version"]; exists && v.Version != platform.Int(c.Data, "version") {
				return nil, nil, status.Error(codes.Aborted, "提问已更新，请重新读取")
			}
			if platform.Text(c.Data, "answer") == "" {
				return nil, nil, bad("answer required")
			}
			d := payload(*v)
			d["answer"] = platform.Text(c.Data, "answer")
			v.Payload = record(v.ID, v.UserID, v.Kind, v.Status, d).Payload
			v.Status = constants.InteractionStatusPublished
			evt := &platform.Event{
				ID:          platform.ID("evt"),
				Type:        constants.EventQAChanged,
				AggregateID: v.ID,
				UserID:      v.UserID,
				Data:        view(*v),
			}
			if v.Kind == constants.InteractionKindReview {
				evt.Type = constants.EventReviewPublished
			}
			return nil, evt, nil
		},
	)
	if e != nil {
		return nil, e
	}
	return view(r), nil
}
func (s *Service) Like(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "interaction.usecase.like")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	target, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if target.Status != constants.InteractionStatusPublished || target.Kind != constants.InteractionKindReview {
		return nil, status.Error(codes.FailedPrecondition, "published review required")
	}
	r := record(
		stable(c.UserID, constants.InteractionKindLike, c.ID),
		c.UserID,
		constants.InteractionKindLike,
		constants.InteractionStatusActive,
		nil,
	)
	r.ParentID = c.ID
	if _, e = s.repo.InsertOnce(ctx, &r, nil); e != nil {
		return nil, e
	}
	count, e := s.repo.Count(ctx, c.ID, constants.InteractionKindLike)
	if e != nil {
		return nil, e
	}
	return platform.Result{"id": c.ID, "helpful_count": count, "liked": true}, nil
}

func (s *Service) seed(context.Context) error                  { return nil }
func (s *Service) event(context.Context, platform.Event) error { return nil }
