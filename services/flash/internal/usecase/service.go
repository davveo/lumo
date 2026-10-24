package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/flash/internal/repository"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

type Service struct {
	Repo  *repository.Repository
	RPC   platform.Caller
	Redis *redis.Client
}

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "usecase.flash."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Action == "list" {
		return s.Repo.List(ctx)
	}
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	switch c.Action {
	case "get":
		v, e := s.Repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if v.UserID != c.UserID && c.Role != "admin" {
			return nil, status.Error(codes.PermissionDenied, "flash request owner mismatch")
		}
		return platform.Map(v), nil
	case "request":
		if c.IdempotencyKey == "" || len(c.IdempotencyKey) > 120 || platform.Text(c.Data, "quote_id") == "" {
			return nil, status.Error(codes.InvalidArgument, "idempotency_key and quote_id required")
		}
		old, e := s.Repo.Find(ctx, c.UserID, c.IdempotencyKey)
		if e != nil {
			return nil, e
		}
		if old != nil {
			return s.Repo.Create(ctx, c)
		}
		if s.RPC == nil {
			return nil, status.Error(codes.Unavailable, "pricing service unavailable")
		}
		q, e := s.RPC.Call(ctx, "pricing", platform.Command{Action: "get", ID: platform.Text(c.Data, "quote_id"), UserID: c.UserID, Role: c.Role})
		if e != nil {
			return nil, e
		}
		if platform.Text(q, "activity_id") != "flash-demo" {
			return nil, status.Error(codes.InvalidArgument, "quote must reference flash-demo activity")
		}
		session, e := s.Repo.Session(ctx, "flash-demo")
		if e != nil {
			return nil, e
		}
		if !session.Active {
			return nil, status.Error(codes.FailedPrecondition, "flash session inactive")
		}
		var quote struct {
			Items []struct {
				SKUID    string `json:"sku_id"`
				Quantity int64  `json:"quantity"`
			} `json:"items"`
		}
		if e = platform.Decode(q, &quote); e != nil {
			return nil, e
		}
		if len(quote.Items) != 1 || quote.Items[0].SKUID != session.SKUID || quote.Items[0].Quantity != 1 {
			return nil, status.Error(codes.InvalidArgument, "quote must contain the session SKU once")
		}
		if s.Redis != nil {
			cacheCtx, rs := platform.Start(ctx, "cache.flash.admission")
			platform.TraceRequest(cacheCtx, platform.Result{"operation": "SETNX", "user_id": c.UserID, "ttl_ms": 1000})
			allowed, er := s.Redis.SetNX(cacheCtx, "flash:admit:"+c.UserID, "1", time.Second).Result()
			platform.TraceResponse(cacheCtx, platform.Result{"allowed": allowed}, er)
			rs.End()
			if er != nil {
				return nil, status.Error(codes.Unavailable, "admission cache unavailable")
			}
			if !allowed {
				return nil, status.Error(codes.ResourceExhausted, "please wait before another flash request")
			}
		}
		return s.Repo.Create(ctx, c)
	}
	return nil, status.Error(codes.Unimplemented, "unsupported action")
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) (traceErr error) {
	if e.Type != "FlashOrderRequested" {
		return nil
	}
	ctx, sp := platform.Start(ctx, "usecase.flash.ProcessRequest")
	defer sp.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	v, er := s.Repo.Get(ctx, e.AggregateID)
	if er != nil {
		return er
	}
	if v.State == "SUCCEEDED" || v.State == "FAILED" {
		return nil
	}
	if er = s.Repo.SetProcessing(ctx, v.ID); er != nil {
		return er
	}
	if s.RPC == nil {
		return status.Error(codes.Unavailable, "checkout unavailable")
	}
	var data map[string]any
	if er = json.Unmarshal([]byte(v.PayloadJSON), &data); er != nil {
		return er
	}
	result, er := s.RPC.Call(ctx, "checkout", platform.Command{Action: "submit", UserID: v.UserID, Role: "system", IdempotencyKey: "flash:" + v.ID, Data: data})
	if er != nil {
		switch status.Code(er) {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Internal, codes.Aborted:
			return er
		}
		return s.Repo.Finish(ctx, v.ID, "", status.Convert(er).Message())
	}
	order := platform.Text(result, "order_id")
	if order == "" {
		return status.Error(codes.Internal, "checkout did not return order_id")
	}
	orderState := platform.Text(result, "status")
	switch orderState {
	case "PENDING_PAYMENT", "PAID_CONFIRMING", "PAID_WAIT_GROUP", "READY_TO_FULFILL", "SHIPPED", "COMPLETED", "PARTIALLY_REFUNDED", "REFUNDED":
	case "CANCELLED", "CLOSED", "CREATE_FAILED":
		return s.Repo.Finish(ctx, v.ID, order, "order creation ended in "+orderState)
	default:
		return status.Error(codes.Aborted, "order creation still processing")
	}
	return s.Repo.Finish(ctx, v.ID, order, "")
}
