package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement/internal/domain"
	"github.com/davveo/lumo/services/engagement/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.PolicyGet")
	defer sp.End()
	// Only public feature switches are returned. Administrative write metadata
	// and per-user subscription records are never part of this endpoint.
	p, v, e := s.repo.AlertPolicy(ctx)
	return platform.Result{"config": p, "version": v}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.PolicyUpsert")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Config repository.AlertPolicy `json:"config"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if in.Config.TTLDays < 1 || in.Config.TTLDays > 90 || in.Config.MaxPerUser < 1 || in.Config.MaxPerUser > 100 {
		return nil, bad("ttl_days must be 1..90 and max_per_user 1..100")
	}
	return s.repo.SaveAlertPolicy(ctx, c, in.Config)
}
func (s *Service) Subscribe(ctx context.Context, c platform.Request) (result platform.Result, err error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.Subscribe")
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, result, err) }()
	if err = platform.RequireUser(c); err != nil {
		return nil, err
	}
	sku, kind := platform.Text(c.Data, "sku_id"), platform.Text(c.Data, "kind")
	target := platform.Int(c.Data, "target_cents")
	if sku == "" || len(sku) > 128 || (kind != constants.SubscriptionKindPrice && kind != constants.SubscriptionKindStock) {
		return nil, bad("valid sku_id and price/stock kind required")
	}
	policy, _, err := s.repo.AlertPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if kind == constants.SubscriptionKindPrice && !policy.PriceEnabled || kind == constants.SubscriptionKindStock && !policy.StockEnabled {
		return nil, status.Error(codes.FailedPrecondition, "this subscription method is disabled")
	}
	clients := platform.NewClients(s.rpc)
	p, err := clients.Catalog.Get(ctx, platform.Request{ID: sku, UserID: c.UserID, Role: c.Role})
	if err != nil {
		return nil, err
	}
	active, _ := p["active"].(bool)
	price := platform.Int(p, "price_cents")
	if !active || price < 1 {
		return nil, status.Error(codes.FailedPrecondition, "product is unavailable")
	}
	if kind == constants.SubscriptionKindPrice {
		if target < 1 || target >= price {
			return nil, bad("target price must be positive and below the current product price")
		}
	} else {
		stock, e := clients.Inventory.Get(ctx, platform.Request{ID: sku, UserID: c.UserID, Role: c.Role})
		if e != nil {
			return nil, e
		}
		if platform.Int(stock, "available") > 0 {
			return nil, status.Error(codes.FailedPrecondition, "product is already in stock")
		}
		target = 0
	}
	now := time.Now().UTC()
	row, e := s.repo.Subscribe(ctx, domain.Subscription{SlotKey: stable(c.UserID, kind, sku), UserID: c.UserID, SKUID: sku, ProductName: platform.Text(p, "name"), Kind: kind, Status: constants.SubscriptionStatusActive, TargetCents: target, PriceCents: price, ExpiresAt: now.Add(time.Duration(policy.TTLDays) * 24 * time.Hour), NextCheckAt: now}, policy.MaxPerUser)
	if e != nil {
		return nil, e
	}
	return platform.Map(row), nil
}
func (s *Service) Subscriptions(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.Subscriptions")
	defer sp.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	return s.subscriptionList(ctx, c, c.UserID)
}
func (s *Service) AdminList(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.AdminList")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.subscriptionList(ctx, c, platform.Text(c.Data, "user_id"))
}
func (s *Service) subscriptionList(ctx context.Context, c platform.Request, owner string) (platform.Result, error) {
	limit, offset := int(platform.Int(c.Data, "limit")), int(platform.Int(c.Data, "offset"))
	if limit == 0 {
		limit = 20
	}
	kind, state, sku := platform.Text(c.Data, "kind"), platform.Text(c.Data, "status"), platform.Text(c.Data, "sku_id")
	if limit < 1 || limit > 100 || offset < 0 || len(owner) > 128 || len(sku) > 128 {
		return nil, bad("invalid subscription filter or pagination")
	}
	if kind != "" && kind != constants.SubscriptionKindPrice && kind != constants.SubscriptionKindStock {
		return nil, bad("invalid subscription kind")
	}
	if state != "" && state != constants.SubscriptionStatusActive && state != constants.SubscriptionStatusTriggered && state != constants.SubscriptionStatusCancelled && state != constants.SubscriptionStatusExpired {
		return nil, bad("invalid subscription status")
	}
	rows, total, e := s.repo.Subscriptions(ctx, owner, sku, kind, state, limit, offset)
	return platform.Result{"items": rows, "total": total, "limit": limit, "offset": offset}, e
}
func (s *Service) Unsubscribe(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.Unsubscribe")
	defer sp.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	row, e := s.repo.CancelSubscription(ctx, c, false)
	if e != nil {
		return nil, e
	}
	return platform.Map(row), nil
}
func (s *Service) AdminCancel(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.AdminCancel")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if r := strings.TrimSpace(platform.Text(c.Data, "reason")); r == "" || len([]rune(r)) > 240 {
		return nil, bad("cancellation reason required, at most 240 characters")
	}
	row, e := s.repo.CancelSubscription(ctx, c, true)
	if e != nil {
		return nil, e
	}
	return platform.Map(row), nil
}

// Tick checks at most 100 due rows with two bounded domain batch reads. Each
// successful check is persisted with its event in one owner transaction.
func (s *Service) Tick(ctx context.Context) error {
	ctx, sp := platform.Start(ctx, "engagement.usecase.TickSubscriptions")
	defer sp.End()
	now := time.Now().UTC()
	rows, e := s.repo.DueSubscriptions(ctx, now)
	if e != nil || len(rows) == 0 {
		return e
	}
	policy, _, e := s.repo.AlertPolicy(ctx)
	if e != nil {
		return e
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.ExpiresAt.After(now) && !seen[r.SKUID] {
			seen[r.SKUID] = true
			ids = append(ids, r.SKUID)
		}
	}
	products, stocks := map[string]map[string]any{}, map[string]map[string]any{}
	if len(ids) > 0 {
		clients := platform.NewClients(s.rpc)
		request := platform.Request{Role: constants.RoleSystem, UserID: constants.RoleSystem, Data: map[string]any{"ids": ids}}
		catalog, e := clients.Catalog.BatchGet(ctx, request)
		if e != nil {
			return e
		}
		inventory, e := clients.Inventory.BatchGet(ctx, request)
		if e != nil {
			return e
		}
		var p, i struct {
			Items []map[string]any `json:"items"`
		}
		if e = platform.Decode(catalog, &p); e != nil {
			return e
		}
		if e = platform.Decode(inventory, &i); e != nil {
			return e
		}
		for _, v := range p.Items {
			products[platform.Text(v, "id")] = v
		}
		for _, v := range i.Items {
			stocks[platform.Text(v, "sku_id")] = v
		}
	}
	for _, r := range rows {
		p := products[r.SKUID]
		active, _ := p["active"].(bool)
		price := platform.Int(p, "price_cents")
		hit := false
		if active && price > 0 && r.ExpiresAt.After(now) {
			if r.Kind == constants.SubscriptionKindPrice {
				hit = policy.PriceEnabled && price <= r.TargetCents
			} else {
				stock, ok := stocks[r.SKUID]
				hit = policy.StockEnabled && ok && platform.Int(stock, "available") > 0
			}
		}
		if e = s.repo.CompleteCheck(ctx, r, now, price, hit); e != nil {
			return e
		}
	}
	return nil
}
