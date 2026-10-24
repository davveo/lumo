package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	switch c.Action {
	case "list":
		kind := platform.Text(c.Data, "kind")
		if kind == "" {
			kind = "faq"
		}
		return s.list(ctx, c, "", kind, "PUBLISHED")
	case "get":
		r, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if r.Status != "PUBLISHED" && c.Role != "admin" {
			return nil, status.Error(codes.NotFound, "content not published")
		}
		return view(r), nil
	case "upsert":
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		title := platform.Text(c.Data, "title")
		body := platform.Text(c.Data, "body")
		kind := platform.Text(c.Data, "kind")
		if kind == "" {
			kind = "faq"
		}
		limit := 20000
		if kind == "storefront" {
			limit = 900000
		}
		if title == "" || body == "" || len(body) > limit {
			return nil, bad("title and body required")
		}
		if c.ID == "" {
			c.ID = platform.ID("content")
		}
		r := record(c.ID, "", kind, "PUBLISHED", map[string]any{"title": title, "body": body, "version": time.Now().Unix()})
		if e := s.repo.Save(ctx, &r, nil); e != nil {
			return nil, e
		}
		return view(r), nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported content action")
	}
}
func (s *Service) seed(ctx context.Context) error {
	if !demo() {
		return nil
	}
	r := record("faq-orders", "", "faq", "PUBLISHED", map[string]any{"title": "如何查看订单和申请售后？", "body": "在个人中心查看订单，付款后可按订单状态申请退款或退货。"})
	_, e := s.repo.InsertOnce(ctx, &r, nil)
	return e
}
func (s *Service) event(context.Context, platform.Event) error { return nil }
