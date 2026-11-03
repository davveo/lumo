package usecase

import (
	"context"

	"github.com/davveo/lumo/internal/commercepolicy"
	"os"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/logistics/internal/domain"
	"github.com/davveo/lumo/services/logistics/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	Repo *repository.Repository
	RPC  platform.Caller
}

func (s *Service) Topics() []string                              { return nil }
func (s *Service) OnEvent(context.Context, platform.Event) error { return nil }
func privileged(c platform.Request) bool {
	return c.Role == constants.RoleAdmin || c.Role == constants.RoleSystem
}
func (s *Service) Create(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.logistics.create")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	if !privileged(c) {
		return nil, status.Error(codes.PermissionDenied, "internal fulfillment permission required")
	}
	if os.Getenv("DEMO_MODE") != "true" && os.Getenv("LOGISTICS_CARRIER_CREATE_URL") == "" {
		return nil, status.Error(
			codes.FailedPrecondition,
			"real carrier adapter not configured; local carrier requires DEMO_MODE=true",
		)
	}
	orderID := platform.Text(c.Data, "order_id")
	if orderID == "" {
		orderID = c.ID
	}
	if c.ID == "" || c.UserID == "" {
		return nil, status.Error(codes.InvalidArgument, "fulfillment id and owner required")
	}
	kind := platform.Text(c.Data, "kind")
	if kind == "" {
		kind = constants.FulfillmentKindOrder
	}
	if old, e := s.Repo.Get(ctx, c.ID); e == nil {
		if old.UserID != c.UserID {
			return nil, status.Error(codes.PermissionDenied, "运单归属不匹配")
		}
		return platform.Map(old), nil
	} else if status.Code(e) != codes.NotFound {
		return nil, e
	}
	number, err := s.Repo.BusinessID(ctx, "tracking", c.ID, "")
	if err != nil {
		return nil, err
	}
	r := &domain.Record{
		ID:         c.ID,
		OrderID:    orderID,
		UserID:     c.UserID,
		Kind:       kind,
		Status:     constants.LogisticsStatusCreated,
		TrackingID: number,
		Provider:   constants.LogisticsProviderLocalDemoCarrier,
	}
	if os.Getenv("LOGISTICS_CARRIER_CREATE_URL") != "" {
		var e error
		r.TrackingID, r.Provider, e = s.createCarrier(ctx, c, orderID)
		if e != nil {
			return nil, e
		}
	}
	if err := s.Repo.Create(ctx, r); err != nil {
		return nil, err
	}
	return platform.Map(r), nil
}

func (s *Service) Deliver(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.logistics.deliver")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	if !privileged(c) {
		return nil, status.Error(codes.PermissionDenied, "carrier callback/admin permission required")
	}
	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if r.Status == constants.TrackingStatusDelivered {
		return platform.Map(r), nil
	}
	out, err := s.TrackingUpdate(
		ctx,
		platform.Request{
			ID:     "warehouse-delivered:" + r.ID,
			UserID: c.UserID,
			Role:   c.Role,
			Data: map[string]any{
				"tracking_id": r.TrackingID,
				"status":      constants.TrackingStatusDelivered,
				"occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
				"description": "仓库确认已签收",
				"_source":     "warehouse",
			},
		},
	)
	// A concurrent delivery can win after the initial read. Its persisted event
	// has a different server timestamp but confirms the same terminal outcome.
	if status.Code(err) == codes.AlreadyExists {
		if current, e := s.Repo.Get(ctx, c.ID); e == nil && current.Status == constants.TrackingStatusDelivered {
			return platform.Map(current), nil
		}
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.logistics.get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if !privileged(c) && (c.UserID == "" || r.UserID != c.UserID) {
		return nil, status.Error(codes.PermissionDenied, "waybill ownership mismatch")
	}
	return platform.Map(r), nil
}

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.logistics.list")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	user := c.UserID
	if privileged(c) {
		user = ""
	}
	rows, err := s.Repo.List(ctx, user)
	return platform.Result{"items": rows}, err
}

func (s *Service) Quote(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.logistics.quote")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}

	policy, e := s.Repo.Policy(ctx)
	if e != nil {
		return nil, e
	}
	var in struct {
		Address  map[string]any `json:"address"`
		Region   string         `json:"region"`
		Subtotal int64          `json:"subtotal_cents"`
		Quantity int64          `json:"quantity"`
	}
	if e = platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if in.Address == nil {
		in.Address = map[string]any{"region": in.Region}
	}
	region := commercepolicy.Region(in.Address)
	if in.Subtotal < 0 || in.Subtotal > 10000000000000 || in.Quantity < 0 || in.Quantity > 500 {
		return nil, status.Error(codes.InvalidArgument, "invalid shipping subtotal or quantity")
	}
	fee, allowed := policy.Quote(region, in.Subtotal, in.Quantity)
	return platform.Result{
		"shipping_cents":    fee,
		"estimated":         region == "",
		"address_confirmed": region != "",
		"promise":           policy.Promise(region, time.Now()),
		"provider":          policy.Name,
		"serviceable":       allowed,
		"region":            region,
		"policy_version":    policy.Version,
		"policy":            policy,
	}, nil
}
