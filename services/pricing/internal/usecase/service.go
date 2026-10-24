package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/pricing/internal/domain"
	"github.com/davveo/lumo/services/pricing/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"sort"
	"time"
)

type Service struct {
	r   *repository.Repository
	rpc platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{r, rpc} }
func (s *Service) Topics() []string                              { return nil }
func (s *Service) OnEvent(context.Context, platform.Event) error { return nil }
func (s *Service) Tick(context.Context) error                    { return nil }
func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "pricing.usecase.Get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	q, err := s.r.Get(ctx, c.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, status.Error(codes.NotFound, "quote not found")
	}
	if err != nil {
		return nil, err
	}
	if q.UserID != c.UserID && c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "quote belongs to another member")
	}
	var out platform.Result
	if err = json.Unmarshal([]byte(q.Payload), &out); err != nil {
		return nil, err
	}
	out["expires_at"] = q.ExpiresAt.Format(time.RFC3339Nano)
	out["user_id"] = q.UserID
	return out, nil
}

func (s *Service) Quote(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "pricing.usecase.Quote")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	var in struct {
		Items    []domain.Item `json:"items"`
		CouponID string        `json:"coupon_id"`
		Points   int64         `json:"points"`
		Addon    string        `json:"addon_sku_id"`
		Activity string        `json:"activity_id"`
		Group    string        `json:"group_id"`
	}
	if err := platform.Decode(c.Data, &in); err != nil {
		return nil, err
	}
	if len(in.Items) == 0 || len(in.Items) > 50 {
		return nil, status.Error(codes.InvalidArgument, "1 to 50 item lines required")
	}
	if in.Points < 0 {
		return nil, status.Error(codes.InvalidArgument, "points cannot be negative")
	}
	if in.Addon != "" && (in.Group != "" || in.Activity != "") {
		return nil, status.Error(codes.FailedPrecondition, "add-on cannot combine with another campaign")
	}
	if in.Group != "" {
		if len(in.Items) != 1 || in.Items[0].Quantity != 1 {
			return nil, status.Error(codes.InvalidArgument, "group order requires one unit of its SKU")
		}
		team, e := platform.NewClients(
			s.rpc,
		).Group.Get(
			ctx,
			platform.Request{
				ID:     in.Group,
				UserID: c.UserID,
				Role:   c.Role,
			},
		)
		if e != nil {
			return nil, e
		}
		if platform.Text(team, "sku_id") != in.Items[0].SKUID ||
			platform.Text(team, "status") != constants.GroupStatusOpen {
			return nil, status.Error(codes.FailedPrecondition, "group SKU or state does not match")
		}
		if in.Activity == "" {
			in.Activity = "group-demo"
		}
	}
	if in.Addon != "" {
		in.Items = append(in.Items, domain.Item{SKUID: in.Addon, Quantity: 1})
	}
	seen := map[string]int64{}
	ids := make([]string, 0, len(in.Items))
	for _, line := range in.Items {
		if _, exists := seen[line.SKUID]; !exists {
			ids = append(ids, line.SKUID)
		}
		seen[line.SKUID] += line.Quantity
		if line.SKUID == "" || line.Quantity < 1 || seen[line.SKUID] > 10 {
			return nil, status.Error(codes.InvalidArgument, "invalid SKU quantity")
		}
	}
	sort.Strings(ids)
	catalog, err := platform.NewClients(
		s.rpc,
	).Catalog.BatchGet(
		ctx,
		platform.Request{
			UserID: c.UserID,
			Role:   c.Role,
			Data:   map[string]any{"ids": ids},
		},
	)
	if err != nil {
		return nil, err
	}
	var catalogRows struct {
		Items []map[string]any `json:"items"`
	}
	if err = platform.Decode(catalog, &catalogRows); err != nil {
		return nil, err
	}
	bySKU := map[string]map[string]any{}
	for _, row := range catalogRows.Items {
		bySKU[platform.Text(row, "id")] = row
	}
	items := make([]domain.Item, 0, len(in.Items))
	var subtotal int64
	var addonAmount int64
	var quantity int64
	for index, line := range in.Items {
		p, exists := bySKU[line.SKUID]
		if !exists {
			return nil, status.Error(codes.NotFound, "SKU unavailable")
		}
		active, _ := p["active"].(bool)
		if !active {
			return nil, status.Error(codes.FailedPrecondition, "product is not available")
		}
		unit := platform.Int(p, "price_cents")
		if unit <= 0 || unit > 10000000000 {
			return nil, status.Error(codes.FailedPrecondition, "invalid product price")
		}
		line.Name = platform.Text(p, "name")
		line.UnitPrice = unit
		line.Amount = unit * line.Quantity
		subtotal += line.Amount
		quantity += line.Quantity
		if in.Addon != "" && index == len(in.Items)-1 {
			addonAmount = line.Amount
		}
		items = append(items, line)
	}
	camp, err := platform.NewClients(
		s.rpc,
	).Campaign.Evaluate(
		ctx,
		platform.Request{
			UserID: c.UserID,
			Role:   c.Role,
			Data: map[string]any{
				"subtotal_cents": subtotal - addonAmount,
				"quantity":       quantity,
				"addon_sku_id":   in.Addon,
				"activity_id":    in.Activity,
			},
		},
	)
	if err != nil {
		return nil, err
	}
	discount := platform.Int(camp, "discount_cents")
	if in.Addon != "" || in.Activity != "" {
		if b, ok := camp["eligible"].(bool); ok && !b {
			return nil, status.Error(
				codes.FailedPrecondition,
				"campaign requirements not met: "+platform.Text(camp, "reason"),
			)
		}
	}
	kind := platform.Text(camp, "kind")
	if (in.Group != "" && kind != constants.CampaignKindGroup) ||
		(kind == constants.CampaignKindGroup && in.Group == "") {
		return nil, status.Error(codes.FailedPrecondition, "group must use its group campaign")
	}
	if kind == constants.CampaignKindAddon && in.Addon == "" {
		return nil, status.Error(codes.FailedPrecondition, "add-on campaign requires its add-on SKU")
	}
	resolvedActivity := platform.Text(camp, "activity_id")
	if resolvedActivity != "" {
		in.Activity = resolvedActivity
	}
	campaignDiscount := discount
	var couponDiscount int64
	if in.CouponID != "" {
		coupons, e := platform.NewClients(s.rpc).Coupon.List(ctx, platform.Request{
			UserID: c.UserID,
			Role:   c.Role,
		})
		if e != nil {
			return nil, e
		}
		raw, _ := json.Marshal(coupons["items"])
		var rows []map[string]any
		_ = json.Unmarshal(raw, &rows)
		found := false
		for _, row := range rows {
			if platform.Text(row, "id") == in.CouponID {
				st := platform.Text(row, "status")
				if st != constants.CouponStatusAvailable && st != constants.CouponStatusAvailableLegacy {
					return nil, status.Error(codes.FailedPrecondition, "coupon not available")
				}
				min := platform.Int(row, "min_cents")
				if min == 0 {
					min = platform.Int(row, "min_spend_cents")
				}
				if subtotal < min {
					return nil, status.Error(codes.FailedPrecondition, "coupon threshold not met")
				}
				value := platform.Int(row, "amount_cents")
				if value == 0 {
					value = platform.Int(row, "discount_cents")
				}
				if value <= 0 {
					return nil, status.Error(codes.FailedPrecondition, "invalid coupon amount")
				}
				discount += value
				couponDiscount = value
				found = true
				break
			}
		}
		if !found {
			return nil, status.Error(codes.NotFound, "coupon not found")
		}
	}
	if in.Points > 0 {
		pts, e := platform.NewClients(s.rpc).Points.Get(ctx, platform.Request{
			UserID: c.UserID,
			Role:   c.Role,
		})
		if e != nil {
			return nil, e
		}
		if in.Points > platform.Int(pts, "available") || in.Points > subtotal/10 {
			return nil, status.Error(codes.FailedPrecondition, "points balance or deduction limit exceeded")
		}
	}
	if discount < 0 || discount+in.Points >= subtotal {
		return nil, status.Error(codes.FailedPrecondition, "discount exceeds order amount")
	}
	id := platform.ID("quote")
	expires := time.Now().UTC().Add(10 * time.Minute)
	out := platform.Result{
		"quote_id":                id,
		"user_id":                 c.UserID,
		"subtotal_cents":          subtotal,
		"main_subtotal_cents":     subtotal - addonAmount,
		"discount_cents":          discount,
		"coupon_discount_cents":   couponDiscount,
		"campaign_discount_cents": campaignDiscount,
		"campaign_policy_version": camp["policy_version"],
		"points":                  in.Points,
		"payable_cents":           subtotal - discount - in.Points,
		"items":                   items,
		"coupon_id":               in.CouponID,
		"activity_id":             in.Activity,
		"group_id":                in.Group,
		"addon_sku_id":            in.Addon,
		"expires_at":              expires.Format(time.RFC3339Nano),
		"shipping_cents":          int64(0),
		"policy_version":          constants.PricingPolicyVersionDemo,
	}
	out["activity_kind"], out["activity_name"] = kind, platform.Text(camp, "name")
	out["pay_rate_bps"], out["min_quantity"] = platform.Int(camp, "pay_rate_bps"), platform.Int(camp, "min_quantity")
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	q := domain.Quote{ID: id, UserID: c.UserID, Payload: string(b), ExpiresAt: expires}
	if err = s.r.Create(ctx, &q); err != nil {
		return nil, err
	}
	return out, nil
}
