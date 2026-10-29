package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase.list")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.ContentKindFaq
	}
	return s.list(ctx, c, "", kind, constants.ContentStatusPublished)
}
func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase.get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	r, e := s.repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if !visibleContent(r) && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.NotFound, "content not published")
	}
	return view(r), nil
}
func (s *Service) Upsert(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "content.usecase.upsert")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	title := platform.Text(c.Data, "title")
	body := platform.Text(c.Data, "body")
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.ContentKindFaq
	}
	if c.ID == categoryNavigationID || kind == constants.ContentKindCategoryNavigation ||
		kind == constants.ContentKindCategories {
		if c.ID != categoryNavigationID || kind != constants.ContentKindStorefront {
			return nil, bad("category navigation requires storefront-categories ID and storefront kind")
		}
		var validTitle bool
		if title, validTitle = categoryText(title, 120, true); !validTitle {
			return nil, bad("category navigation title must contain 1..120 characters")
		}
		var err error
		body, err = validateCategoryNavigation(body)
		if err != nil {
			return nil, err
		}
	}
	if c.ID == "storefront-main" {
		if err := validateStorefrontSettings(body); err != nil {
			return nil, err
		}
	}
	limit := 20000
	if kind == constants.ContentKindStorefront {
		limit = 900000
	}
	if title == "" || body == "" || len(body) > limit {
		return nil, bad("title and body required")
	}
	if c.ID == "" {
		var err error
		c.ID, err = s.repo.BusinessID(ctx, "content", "", "")
		if err != nil {
			return nil, err
		}
	}
	r := record(
		c.ID,
		"",
		kind,
		constants.ContentStatusPublished,
		map[string]any{"title": title, "body": body, "version": time.Now().Unix()},
	)
	if e := s.repo.Save(ctx, &r, nil); e != nil {
		return nil, e
	}
	return view(r), nil
}

func (s *Service) seed(ctx context.Context) error {
	if !demo() {
		return nil
	}
	r := record(
		"faq-orders",
		"",
		constants.ContentKindFaq,
		constants.ContentStatusPublished,
		map[string]any{"title": "如何查看订单和申请售后？", "body": "在个人中心查看订单，付款后可按订单状态申请退款或退货。"},
	)
	if _, e := s.repo.InsertOnce(ctx, &r, nil); e != nil {
		return e
	}
	return s.seedPopups(ctx)
}
func (s *Service) event(context.Context, platform.Event) error { return nil }
