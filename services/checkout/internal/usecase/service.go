package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/checkout/internal/domain"
	"github.com/davveo/lumo/services/checkout/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	r   *repository.Repository
	rpc platform.Caller
}

func New(r *repository.Repository, rpc platform.Caller) *Service { return &Service{r, rpc} }
func (s *Service) Topics() []string {
	return []string{
		constants.EventOrderPaymentAccepted,
		constants.EventOrderClosed,
		constants.EventGroupFormed,
		constants.EventGroupFailed,
	}
}

func (s *Service) Quote(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "checkout.usecase.Quote")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	return platform.NewClients(s.rpc).Pricing.Quote(ctx, c)
}
func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "checkout.usecase.Get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	a, err := s.r.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if a.UserID != c.UserID && c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "checkout belongs to another member")
	}
	return platform.Map(a), nil
}
func (s *Service) Paid(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "checkout.usecase.Paid")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Role != constants.RoleSystem && c.Role != constants.RoleAdmin {
		return nil, status.Error(codes.PermissionDenied, "internal payment workflow")
	}
	return s.resumeWorkflow(ctx, c.ID, c.UserID, constants.CheckoutModePaid)
}

func (s *Service) Submit(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "checkout.usecase.Submit")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	if c.IdempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "Idempotency-Key required")
	}
	hash := sha256.Sum256([]byte(c.UserID + "\x00" + c.IdempotencyKey))
	key := hex.EncodeToString(hash[:])
	body, err := json.Marshal(c.Data)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid submit payload")
	}
	bodyHash := sha256.Sum256(body)
	requestHash := hex.EncodeToString(bodyHash[:])
	id := "ord_" + key[:32]
	old, err := s.r.Get(ctx, id)
	if err == nil {
		if old.QuoteID != platform.Text(c.Data, "quote_id") ||
			(old.RequestHash != "" && old.RequestHash != requestHash) {
			return nil, status.Error(codes.AlreadyExists, "idempotency key reused with a different quote")
		}
		return s.resumeWorkflow(ctx, id, c.UserID, constants.CheckoutModeCreate)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	q, err := platform.NewClients(
		s.rpc,
	).Pricing.Get(
		ctx,
		platform.Request{
			ID:     platform.Text(c.Data, "quote_id"),
			UserID: c.UserID,
			Role:   c.Role,
		},
	)
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, platform.Text(q, "expires_at"))
	if err != nil || time.Now().After(t) {
		return nil, status.Error(codes.FailedPrecondition, "quote expired; request a new quote")
	}
	var payload struct {
		Items []struct {
			SKUID    string `json:"sku_id"`
			Quantity int64  `json:"quantity"`
			Unit     int64  `json:"unit_price_cents"`
		} `json:"items"`
	}
	if err = platform.Decode(q, &payload); err != nil {
		return nil, err
	}
	if len(payload.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty quote")
	}
	ids, seen := []string{}, map[string]bool{}
	for _, item := range payload.Items {
		if !seen[item.SKUID] {
			seen[item.SKUID] = true
			ids = append(ids, item.SKUID)
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
	var quantity int64
	for _, item := range payload.Items {
		quantity += item.Quantity
		p, exists := bySKU[item.SKUID]
		if !exists {
			return nil, status.Error(codes.NotFound, "SKU unavailable")
		}
		active, _ := p["active"].(bool)
		if !active || platform.Int(p, "price_cents") != item.Unit {
			return nil, status.Error(codes.FailedPrecondition, "catalog changed; requote required")
		}
	}
	if platform.Text(q, "activity_id") != "" {
		mainSubtotal := platform.Int(q, "main_subtotal_cents")
		if mainSubtotal == 0 {
			mainSubtotal = platform.Int(q, "subtotal_cents")
		}
		current, e := platform.NewClients(
			s.rpc,
		).Campaign.Evaluate(
			ctx,
			platform.Request{
				UserID: c.UserID,
				Role:   c.Role,
				Data: map[string]any{
					"subtotal_cents": mainSubtotal,
					"quantity":       quantity,
					"addon_sku_id":   q["addon_sku_id"],
					"activity_id":    q["activity_id"],
				},
			},
		)
		if e != nil {
			return nil, e
		}
		eligible, _ := current["eligible"].(bool)
		if !eligible || platform.Int(current, "discount_cents") != platform.Int(q, "campaign_discount_cents") ||
			platform.Text(current, "policy_version") != platform.Text(q, "campaign_policy_version") {
			return nil, status.Error(codes.FailedPrecondition, "campaign changed; requote required")
		}
	}
	source := platform.Text(c.Data, "source")
	if source == "" {
		source = constants.OrderSourceDirect
	}
	if source != constants.OrderSourceCart && source != constants.OrderSourceDirect {
		return nil, status.Error(codes.InvalidArgument, "source must be cart or direct")
	}
	note := strings.TrimSpace(platform.Text(c.Data, "note"))
	if utf8.RuneCountInString(note) > 1000 {
		return nil, status.Error(codes.InvalidArgument, "order note exceeds1000characters")
	}
	address := c.Data["address"]
	if aid := platform.Text(c.Data, "address_id"); aid != "" {
		list, e := platform.NewClients(s.rpc).Address.List(ctx, platform.Request{
			UserID: c.UserID,
			Role:   c.Role,
		})
		if e != nil {
			return nil, e
		}
		b, _ := json.Marshal(list["items"])
		var rows []map[string]any
		_ = json.Unmarshal(b, &rows)
		found := false
		for _, row := range rows {
			if platform.Text(row, "id") == aid {
				address = row
				found = true
				break
			}
		}
		if !found {
			return nil, status.Error(codes.NotFound, "member address not found")
		}
	}
	// A physical order must retain a validated address snapshot. Only the
	// selected member-owned record or explicitly supplied shipping fields are
	// accepted; no arbitrary/null object becomes an order address.
	var ship struct {
		Recipient  string `json:"recipient"`
		Phone      string `json:"phone"`
		Address    string `json:"address"`
		City       string `json:"city"`
		Province   string `json:"province"`
		District   string `json:"district"`
		PostalCode string `json:"postal_code"`
	}
	encoded, e := json.Marshal(address)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid shipping address")
	}
	if e = json.Unmarshal(encoded, &ship); e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid shipping address fields")
	}
	ship.Recipient = strings.TrimSpace(ship.Recipient)
	ship.Phone = strings.TrimSpace(ship.Phone)
	ship.Address = strings.TrimSpace(ship.Address)
	if ship.Recipient == "" || utf8.RuneCountInString(ship.Recipient) > 100 || len(ship.Phone) < 5 ||
		len(ship.Phone) > 30 ||
		ship.Address == "" ||
		utf8.RuneCountInString(ship.Address) > 1000 ||
		utf8.RuneCountInString(ship.City) > 100 ||
		utf8.RuneCountInString(ship.Province) > 100 ||
		utf8.RuneCountInString(ship.District) > 100 ||
		len(ship.PostalCode) > 30 {
		return nil, status.Error(codes.InvalidArgument, "valid recipient, phone and shipping address required")
	}
	address = ship
	qb, _ := json.Marshal(q)
	ab, _ := json.Marshal(address)
	a := domain.Saga{
		ID:          id,
		UserID:      c.UserID,
		RequestKey:  key,
		RequestHash: requestHash,
		QuoteID:     platform.Text(q, "quote_id"),
		Quote:       string(qb),
		Address:     string(ab),
		Note:        note,
		Source:      source,
		State:       constants.CheckoutStateCreating,
		Steps:       "{}",
		LeaseUntil:  time.Now().UTC(),
	}
	if err = s.r.Ensure(ctx, &a); err != nil {
		return nil, err
	}
	return s.resumeWorkflow(ctx, id, c.UserID, constants.CheckoutModeCreate)
}
func (s *Service) resumeWorkflow(ctx context.Context, id, user, mode string) (platform.Result, error) {
	owner := platform.ID("lease")
	ok, err := s.r.Claim(ctx, id, owner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return platform.Result{
			"order_id": id,
			"status":   constants.CheckoutStateProcessing,
		}, status.Error(
			codes.Aborted,
			"checkout workflow is already processing; query original order",
		)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = s.r.Release(releaseCtx, id, owner)
	}()
	a, err := s.r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.UserID != user {
		return nil, status.Error(codes.PermissionDenied, "checkout belongs to another member")
	}
	var q map[string]any
	var steps map[string]string
	_ = json.Unmarshal([]byte(a.Quote), &q)
	_ = json.Unmarshal([]byte(a.Steps), &steps)
	if steps == nil {
		steps = map[string]string{}
	}
	save := func() error { b, _ := json.Marshal(steps); a.Steps = string(b); return s.r.Save(ctx, &a, owner) }
	if mode == constants.CheckoutModeCreate {
		if a.State != constants.CheckoutStateCreating {
			return platform.Result{"order_id": id, "status": a.State}, nil
		}
		var address any
		_ = json.Unmarshal([]byte(a.Address), &address)
		if _, err = platform.NewClients(s.rpc).Order.Create(ctx, platform.Request{
			ID:     id,
			UserID: user,
			Role:   constants.RoleSystem,
			Data:   map[string]any{"quote": q, "address": address, "note": a.Note, "source": a.Source},
		}); err != nil {
			return nil, err
		}
		clients := platform.NewClients(s.rpc)
		type reservation struct {
			name    string
			reserve platform.Operation
			data    map[string]any
		}
		resources := []reservation{{
			name:    constants.ServiceInventory,
			reserve: clients.Inventory.Reserve,
			data:    map[string]any{"order_id": id, "lines": q["items"]},
		}}
		if platform.Text(q, "activity_id") != "" {
			resources = append(resources, reservation{
				name:    constants.ServiceCampaign,
				reserve: clients.Campaign.Reserve,
				data:    map[string]any{"order_id": id, "activity_id": q["activity_id"], "quantity": int64(1)},
			})
		}
		if platform.Text(q, "group_id") != "" {
			resources = append(resources, reservation{
				name:    constants.ServiceGroup,
				reserve: clients.Group.Reserve,
				data:    map[string]any{"order_id": id, "group_id": q["group_id"]},
			})
		}
		if platform.Text(q, "coupon_id") != "" {
			resources = append(resources, reservation{
				name:    constants.ServiceCoupon,
				reserve: clients.Coupon.Reserve,
				data: map[string]any{
					"order_id": id, "coupon_id": q["coupon_id"], "subtotal_cents": q["subtotal_cents"],
				},
			})
		}
		if platform.Int(q, "points") > 0 {
			resources = append(resources, reservation{
				name:    constants.ServicePoints,
				reserve: clients.Points.Reserve,
				data:    map[string]any{"order_id": id, "amount": q["points"]},
			})
		}
		for _, resource := range resources {
			if steps[resource.name] == constants.CheckoutStepStatusReserved {
				continue
			}
			steps[resource.name] = constants.CheckoutStepStatusTrying
			if err = save(); err != nil {
				return nil, err
			}
			reserved, e := resource.reserve(ctx, platform.Request{
				ID:     id,
				UserID: user,
				Role:   constants.RoleSystem,
				Data:   resource.data,
			})
			if e == nil && resource.name == constants.ServiceCoupon &&
				platform.Int(reserved, "discount_cents") != platform.Int(q, "coupon_discount_cents") {
				e = status.Error(codes.FailedPrecondition, "coupon changed; requote required")
			}
			if e != nil {
				steps[resource.name] = constants.CheckoutStepStatusUnknown
				a.State = constants.CheckoutStateCancelling
				a.LastError = status.Code(e).String()
				if err = save(); err != nil {
					return nil, err
				}
				_ = s.releaseResources(ctx, &a, q, steps, save)
				_, _ = platform.NewClients(
					s.rpc,
				).Order.CreateFailed(
					ctx,
					platform.Request{
						ID:     id,
						UserID: user,
						Role:   constants.RoleSystem,
						Data:   nil,
					},
				)
				return nil, e
			}
			steps[resource.name] = constants.CheckoutStepStatusReserved
			if err = save(); err != nil {
				return nil, err
			}
		}
		if _, err = platform.NewClients(s.rpc).Order.Pending(ctx, platform.Request{
			ID:     id,
			UserID: user,
			Role:   constants.RoleSystem,
			Data:   nil,
		}); err != nil {
			return nil, err
		}
		a.State = constants.CheckoutStatePendingPayment
		if err = save(); err != nil {
			return nil, err
		}
		return platform.Result{"order_id": id, "status": a.State}, nil
	}
	if mode == constants.CheckoutModeRelease || a.State == constants.CheckoutStateCancelling {
		a.State = constants.CheckoutStateCancelling
		if err = save(); err != nil {
			return nil, err
		}
		err = s.releaseResources(ctx, &a, q, steps, save)
		return platform.Result{"order_id": id, "status": a.State}, err
	}
	if mode == constants.CheckoutModePaid {
		if a.State == constants.CheckoutStateReadyToFulfill {
			return platform.Result{"order_id": id, "status": a.State}, nil
		}
		o, e := platform.NewClients(
			s.rpc,
		).Order.Get(
			ctx,
			platform.Request{
				ID:     id,
				UserID: user,
				Role:   constants.RoleSystem,
				Data:   nil,
			},
		)
		if e != nil {
			return nil, e
		}
		st := platform.Text(o, "status")
		if st != constants.OrderStatusPaidConfirming && st != constants.OrderStatusPaidWaitGroup &&
			st != constants.OrderStatusReadyToFulfill {
			return nil, status.Error(codes.FailedPrecondition, "order has not accepted payment")
		}
		a.State = constants.CheckoutStatePaidConfirming
		if err = save(); err != nil {
			return nil, err
		}
		clients := platform.NewClients(s.rpc)
		for _, resource := range []struct {
			name    string
			confirm platform.Operation
		}{
			{constants.ServiceCoupon, clients.Coupon.Confirm},
			{constants.ServicePoints, clients.Points.Confirm},
			{constants.ServiceCampaign, clients.Campaign.Confirm},
		} {
			if steps[resource.name] == "" || steps[resource.name] == constants.CheckoutStepStatusConfirmed {
				continue
			}
			if _, err = resource.confirm(ctx, platform.Request{
				ID:     id,
				UserID: user,
				Role:   constants.RoleSystem,
			}); err != nil {
				return nil, err
			}
			steps[resource.name] = constants.CheckoutStepStatusConfirmed
			if err = save(); err != nil {
				return nil, err
			}
		}
		if gid := platform.Text(q, "group_id"); gid != "" {
			team, e := platform.NewClients(
				s.rpc,
			).Group.Paid(
				ctx,
				platform.Request{
					ID:     id,
					UserID: user,
					Role:   constants.RoleSystem,
					Data:   map[string]any{"group_id": gid},
				},
			)
			if e != nil {
				return nil, e
			}
			if platform.Text(team, "status") == constants.GroupStatusFailed ||
				platform.Text(team, "status") == constants.GroupStatusLegacyFailed {
				if _, e = platform.NewClients(s.rpc).Order.GroupFailed(ctx, platform.Request{
					ID:     id,
					UserID: user,
					Role:   constants.RoleSystem,
					Data:   nil,
				}); e != nil {
					return nil, e
				}
				if _, e = platform.NewClients(s.rpc).Payment.Close(ctx, platform.Request{
					ID:     id,
					UserID: user,
					Role:   constants.RoleSystem,
					Data:   nil,
				}); e != nil {
					return nil, e
				}
				a.State = constants.CheckoutStateCancelling
				if e = save(); e != nil {
					return nil, e
				}
				e = s.releaseResources(ctx, &a, q, steps, save)
				return platform.Result{"order_id": id, "status": a.State}, e
			}
			formed := platform.Text(team, "status") == constants.GroupStatusFormed ||
				platform.Text(team, "status") == constants.GroupStatusLegacyFormed
			if b, _ := team["formed"].(bool); b {
				formed = true
			}
			if !formed {
				a.State = constants.CheckoutStatePaidWaitGroup
				if _, e = platform.NewClients(s.rpc).Order.WaitGroup(ctx, platform.Request{
					ID:     id,
					UserID: user,
					Role:   constants.RoleSystem,
					Data:   nil,
				}); e != nil {
					return nil, e
				}
				if e = save(); e != nil {
					return nil, e
				}
				return platform.Result{"order_id": id, "status": a.State}, nil
			}
			if _, e = platform.NewClients(s.rpc).Order.GroupFormed(ctx, platform.Request{
				ID:     id,
				UserID: user,
				Role:   constants.RoleSystem,
				Data:   nil,
			}); e != nil {
				return nil, e
			}
		}
		if steps[constants.ServiceInventory] != constants.CheckoutStepStatusConfirmed {
			if _, err = platform.NewClients(s.rpc).Inventory.Confirm(ctx, platform.Request{
				ID:     id,
				UserID: user,
				Role:   constants.RoleSystem,
				Data:   nil,
			}); err != nil {
				return nil, err
			}
			steps[constants.ServiceInventory] = constants.CheckoutStepStatusConfirmed
			if err = save(); err != nil {
				return nil, err
			}
		}
		if _, err = platform.NewClients(s.rpc).Order.Ready(ctx, platform.Request{
			ID:     id,
			UserID: user,
			Role:   constants.RoleSystem,
			Data:   nil,
		}); err != nil {
			return nil, err
		}
		a.State = constants.CheckoutStateReadyToFulfill
		if err = save(); err != nil {
			return nil, err
		}
		return platform.Result{"order_id": id, "status": a.State}, nil
	}
	return nil, status.Error(codes.InvalidArgument, "invalid workflow mode")
}

func (s *Service) releaseResources(
	ctx context.Context,
	a *domain.Saga,
	q map[string]any,
	steps map[string]string,
	save func() error,
) error {
	clients := platform.NewClients(s.rpc)
	for _, resource := range []struct {
		name   string
		cancel platform.Operation
	}{
		{constants.ServicePoints, clients.Points.Cancel},
		{constants.ServiceCoupon, clients.Coupon.Cancel},
		{constants.ServiceGroup, clients.Group.Cancel},
		{constants.ServiceCampaign, clients.Campaign.Cancel},
		{constants.ServiceInventory, clients.Inventory.Cancel},
	} {
		st := steps[resource.name]
		if st == "" || st == constants.CheckoutStepStatusCancelled {
			continue
		}
		if st == constants.CheckoutStepStatusConfirmed && resource.name != constants.ServiceInventory {
			continue
		}
		if resource.name == constants.ServiceInventory && st == constants.CheckoutStepStatusConfirmed {
			if _, err := platform.NewClients(s.rpc).Fulfillment.Cancel(ctx, platform.Request{
				ID:     a.ID,
				UserID: a.UserID,
				Role:   constants.RoleSystem,
				Data:   nil,
			}); err != nil {
				return err
			}
		}
		if _, err := resource.cancel(ctx, platform.Request{
			ID:     a.ID,
			UserID: a.UserID,
			Role:   constants.RoleSystem,
		}); err != nil {
			if resource.name != constants.ServiceGroup || status.Code(err) != codes.FailedPrecondition {
				return err
			}
		}
		steps[resource.name] = constants.CheckoutStepStatusCancelled
		if err := save(); err != nil {
			return err
		}
	}
	a.State = constants.CheckoutStateCancelled
	return save()
}
func (s *Service) Cancel(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "checkout.usecase.Cancel")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	o, err := platform.NewClients(s.rpc).Order.Get(ctx, platform.Request{
		ID:     c.ID,
		UserID: c.UserID,
		Role:   c.Role,
	})
	if err != nil {
		return nil, err
	}
	user := platform.Text(o, "user_id")
	st := platform.Text(o, "status")
	if st == constants.OrderStatusClosed || st == constants.OrderStatusCreateFailed {
		return s.resumeWorkflow(ctx, c.ID, user, constants.CheckoutModeRelease)
	}
	if _, err = platform.NewClients(s.rpc).Order.CloseBegin(ctx, platform.Request{
		ID:     c.ID,
		UserID: user,
		Role:   constants.RoleSystem,
		Data:   nil,
	}); err != nil {
		return nil, err
	}
	p, err := platform.NewClients(
		s.rpc,
	).Payment.Close(
		ctx,
		platform.Request{
			ID:     c.ID,
			UserID: user,
			Role:   constants.RoleSystem,
			Data:   nil,
		},
	)
	if err != nil {
		return nil, err
	}
	paid, _ := p["paid"].(bool)
	if paid {
		p["payment_id"] = p["id"]
		accept, e := platform.NewClients(
			s.rpc,
		).Order.AcceptPayment(
			ctx,
			platform.Request{
				ID:     c.ID,
				UserID: user,
				Role:   constants.RoleSystem,
				Data:   p,
			},
		)
		if e != nil {
			err = e
			return nil, err
		}
		if accepted, _ := accept["accepted"].(bool); !accepted {
			return s.resumeWorkflow(ctx, c.ID, user, constants.CheckoutModeRelease)
		}
		return s.resumeWorkflow(ctx, c.ID, user, constants.CheckoutModePaid)
	}
	if _, err = platform.NewClients(s.rpc).Order.CloseComplete(ctx, platform.Request{
		ID:     c.ID,
		UserID: user,
		Role:   constants.RoleSystem,
		Data:   nil,
	}); err != nil {
		return nil, err
	}
	return s.resumeWorkflow(ctx, c.ID, user, constants.CheckoutModeRelease)
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) (traceErr error) {
	ctx, sp := platform.Start(ctx, "checkout.usecase.OnEvent."+e.Type)
	defer sp.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	if e.Type == constants.EventOrderPaymentAccepted {
		id := platform.Text(e.Data, "order_id")
		if id == "" {
			id = e.AggregateID
		}
		_, err := s.Paid(ctx, platform.Request{
			ID:     id,
			UserID: e.UserID,
			Role:   constants.RoleSystem,
		})
		return err
	}
	if e.Type == constants.EventOrderClosed {
		id := platform.Text(e.Data, "order_id")
		if id == "" {
			id = e.AggregateID
		}
		_, err := s.resumeWorkflow(ctx, id, e.UserID, constants.CheckoutModeRelease)
		return err
	}
	if e.Type == constants.EventGroupFormed || e.Type == constants.EventGroupFailed {
		ids := []string{}
		if id := platform.Text(e.Data, "order_id"); id != "" {
			ids = append(ids, id)
		}
		b, _ := json.Marshal(e.Data["order_ids"])
		var other []string
		_ = json.Unmarshal(b, &other)
		ids = append(ids, other...)
		if len(ids) == 0 {
			ids = append(ids, e.AggregateID)
		}
		for _, id := range ids {
			a, err := s.r.Get(ctx, id)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if e.Type == constants.EventGroupFormed {
				if _, err = platform.NewClients(s.rpc).Order.GroupFormed(ctx, platform.Request{
					ID:     id,
					UserID: a.UserID,
					Role:   constants.RoleSystem,
					Data:   nil,
				}); err != nil {
					return err
				}
				if _, err = s.Paid(ctx, platform.Request{
					ID:     id,
					UserID: a.UserID,
					Role:   constants.RoleSystem,
				}); err != nil {
					return err
				}
			} else {
				if _, err = platform.NewClients(s.rpc).Order.GroupFailed(ctx, platform.Request{
					ID:     id,
					UserID: a.UserID,
					Role:   constants.RoleSystem,
					Data:   nil,
				}); err != nil {
					return err
				}
				if _, err = platform.NewClients(s.rpc).Payment.Close(ctx, platform.Request{
					ID:     id,
					UserID: a.UserID,
					Role:   constants.RoleSystem,
					Data:   nil,
				}); err != nil {
					return err
				}
				if _, err = s.resumeWorkflow(ctx, id, a.UserID, constants.CheckoutModeRelease); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return nil
}
func (s *Service) Tick(ctx context.Context) error {
	rows, err := s.r.Pending(ctx)
	if err != nil {
		return err
	}
	for _, a := range rows {
		mode := constants.CheckoutModeCreate
		switch a.State {
		case constants.CheckoutStateCancelling:
			mode = constants.CheckoutModeRelease
		case constants.CheckoutStatePaidConfirming, constants.CheckoutStatePaidWaitGroup:
			mode = constants.CheckoutModePaid
		}
		var resumeErr error
		if mode == constants.CheckoutModePaid {
			_, resumeErr = s.Paid(ctx, platform.Request{
				ID:     a.ID,
				UserID: a.UserID,
				Role:   constants.RoleSystem,
			})
		} else {
			_, resumeErr = s.resumeWorkflow(ctx, a.ID, a.UserID, mode)
		}
		if resumeErr != nil && status.Code(resumeErr) != codes.Aborted &&
			status.Code(resumeErr) != codes.FailedPrecondition {
			return resumeErr
		}
	}
	return nil
}
