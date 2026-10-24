package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	return []string{"OrderPaymentAccepted", "OrderClosed", "GroupFormed", "GroupFailed"}
}
func (s *Service) call(ctx context.Context, target, action, id, user string, data map[string]any) (platform.Result, error) {
	return s.rpc.Call(ctx, target, platform.Command{Action: action, ID: id, UserID: user, Role: "system", Data: data})
}
func (s *Service) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, sp := platform.Start(ctx, "checkout.usecase."+c.Action)
	defer sp.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	switch c.Action {
	case "quote":
		c.Action = "quote"
		return s.rpc.Call(ctx, "pricing", c)
	case "submit":
		return s.submit(ctx, c)
	case "get":
		a, err := s.r.Get(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		if a.UserID != c.UserID && c.Role != "system" && c.Role != "admin" {
			return nil, status.Error(codes.PermissionDenied, "checkout belongs to another member")
		}
		return platform.Map(a), nil
	case "paid":
		if c.Role != "system" && c.Role != "admin" {
			return nil, status.Error(codes.PermissionDenied, "internal payment workflow")
		}
		return s.run(ctx, c.ID, c.UserID, "paid")
	case "cancel":
		return s.cancel(ctx, c)
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported checkout action")
	}
}
func (s *Service) submit(ctx context.Context, c platform.Command) (platform.Result, error) {
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
		if old.QuoteID != platform.Text(c.Data, "quote_id") || (old.RequestHash != "" && old.RequestHash != requestHash) {
			return nil, status.Error(codes.AlreadyExists, "idempotency key reused with a different quote")
		}
		return s.run(ctx, id, c.UserID, "create")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	q, err := s.rpc.Call(ctx, "pricing", platform.Command{Action: "get", ID: platform.Text(c.Data, "quote_id"), UserID: c.UserID, Role: c.Role})
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
	catalog, err := s.rpc.Call(ctx, "catalog", platform.Command{Action: "batch_get", UserID: c.UserID, Role: c.Role, Data: map[string]any{"ids": ids}})
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
		current, e := s.rpc.Call(ctx, "campaign", platform.Command{Action: "evaluate", UserID: c.UserID, Role: c.Role, Data: map[string]any{"subtotal_cents": mainSubtotal, "quantity": quantity, "addon_sku_id": q["addon_sku_id"], "activity_id": q["activity_id"]}})
		if e != nil {
			return nil, e
		}
		eligible, _ := current["eligible"].(bool)
		if !eligible || platform.Int(current, "discount_cents") != platform.Int(q, "campaign_discount_cents") || platform.Text(current, "policy_version") != platform.Text(q, "campaign_policy_version") {
			return nil, status.Error(codes.FailedPrecondition, "campaign changed; requote required")
		}
	}
	source := platform.Text(c.Data, "source")
	if source == "" {
		source = "direct"
	}
	if source != "cart" && source != "direct" {
		return nil, status.Error(codes.InvalidArgument, "source must be cart or direct")
	}
	note := strings.TrimSpace(platform.Text(c.Data, "note"))
	if utf8.RuneCountInString(note) > 1000 {
		return nil, status.Error(codes.InvalidArgument, "order note exceeds1000characters")
	}
	address := c.Data["address"]
	if aid := platform.Text(c.Data, "address_id"); aid != "" {
		list, e := s.rpc.Call(ctx, "address", platform.Command{Action: "list", UserID: c.UserID, Role: c.Role})
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
	if ship.Recipient == "" || utf8.RuneCountInString(ship.Recipient) > 100 || len(ship.Phone) < 5 || len(ship.Phone) > 30 || ship.Address == "" || utf8.RuneCountInString(ship.Address) > 1000 || utf8.RuneCountInString(ship.City) > 100 || utf8.RuneCountInString(ship.Province) > 100 || utf8.RuneCountInString(ship.District) > 100 || len(ship.PostalCode) > 30 {
		return nil, status.Error(codes.InvalidArgument, "valid recipient, phone and shipping address required")
	}
	address = ship
	qb, _ := json.Marshal(q)
	ab, _ := json.Marshal(address)
	a := domain.Saga{ID: id, UserID: c.UserID, RequestKey: key, RequestHash: requestHash, QuoteID: platform.Text(q, "quote_id"), Quote: string(qb), Address: string(ab), Note: note, Source: source, State: "CREATING", Steps: "{}", LeaseUntil: time.Now().UTC()}
	if err = s.r.Ensure(ctx, &a); err != nil {
		return nil, err
	}
	return s.run(ctx, id, c.UserID, "create")
}
func (s *Service) run(ctx context.Context, id, user, mode string) (platform.Result, error) {
	owner := platform.ID("lease")
	ok, err := s.r.Claim(ctx, id, owner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return platform.Result{"order_id": id, "status": "PROCESSING"}, status.Error(codes.Aborted, "checkout workflow is already processing; query original order")
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
	if mode == "create" {
		if a.State != "CREATING" {
			return platform.Result{"order_id": id, "status": a.State}, nil
		}
		var address any
		_ = json.Unmarshal([]byte(a.Address), &address)
		if _, err = s.call(ctx, "order", "create", id, user, map[string]any{"quote": q, "address": address, "note": a.Note, "source": a.Source}); err != nil {
			return nil, err
		}
		resources := []string{"inventory"}
		if platform.Text(q, "activity_id") != "" {
			resources = append(resources, "campaign")
		}
		if platform.Text(q, "group_id") != "" {
			resources = append(resources, "group")
		}
		if platform.Text(q, "coupon_id") != "" {
			resources = append(resources, "coupon")
		}
		if platform.Int(q, "points") > 0 {
			resources = append(resources, "points")
		}
		for _, resource := range resources {
			if steps[resource] == "RESERVED" {
				continue
			}
			steps[resource] = "TRYING"
			if err = save(); err != nil {
				return nil, err
			}
			data := map[string]any{"order_id": id}
			switch resource {
			case "inventory":
				data["lines"] = q["items"]
			case "campaign":
				data["activity_id"] = q["activity_id"]
				data["quantity"] = int64(1)
			case "group":
				data["group_id"] = q["group_id"]
			case "coupon":
				data["coupon_id"] = q["coupon_id"]
				data["subtotal_cents"] = q["subtotal_cents"]
			case "points":
				data["amount"] = q["points"]
			}
			reserved, e := s.call(ctx, resource, "reserve", id, user, data)
			if e == nil && resource == "coupon" && platform.Int(reserved, "discount_cents") != platform.Int(q, "coupon_discount_cents") {
				e = status.Error(codes.FailedPrecondition, "coupon changed; requote required")
			}
			if e != nil {
				steps[resource] = "UNKNOWN"
				a.State = "CANCELLING"
				a.LastError = status.Code(e).String()
				if err = save(); err != nil {
					return nil, err
				}
				_ = s.releaseResources(ctx, &a, q, steps, save)
				_, _ = s.call(ctx, "order", "create_failed", id, user, nil)
				return nil, e
			}
			steps[resource] = "RESERVED"
			if err = save(); err != nil {
				return nil, err
			}
		}
		if _, err = s.call(ctx, "order", "pending", id, user, nil); err != nil {
			return nil, err
		}
		a.State = "PENDING_PAYMENT"
		if err = save(); err != nil {
			return nil, err
		}
		return platform.Result{"order_id": id, "status": a.State}, nil
	}
	if mode == "release" || a.State == "CANCELLING" {
		a.State = "CANCELLING"
		if err = save(); err != nil {
			return nil, err
		}
		err = s.releaseResources(ctx, &a, q, steps, save)
		return platform.Result{"order_id": id, "status": a.State}, err
	}
	if mode == "paid" {
		if a.State == "READY_TO_FULFILL" {
			return platform.Result{"order_id": id, "status": a.State}, nil
		}
		o, e := s.call(ctx, "order", "get", id, user, nil)
		if e != nil {
			return nil, e
		}
		st := platform.Text(o, "status")
		if st != "PAID_CONFIRMING" && st != "PAID_WAIT_GROUP" && st != "READY_TO_FULFILL" {
			return nil, status.Error(codes.FailedPrecondition, "order has not accepted payment")
		}
		a.State = "PAID_CONFIRMING"
		if err = save(); err != nil {
			return nil, err
		}
		for _, resource := range []string{"coupon", "points", "campaign"} {
			if steps[resource] == "" || steps[resource] == "CONFIRMED" {
				continue
			}
			if _, err = s.call(ctx, resource, "confirm", id, user, nil); err != nil {
				return nil, err
			}
			steps[resource] = "CONFIRMED"
			if err = save(); err != nil {
				return nil, err
			}
		}
		if gid := platform.Text(q, "group_id"); gid != "" {
			team, e := s.call(ctx, "group", "paid", id, user, map[string]any{"group_id": gid})
			if e != nil {
				return nil, e
			}
			if platform.Text(team, "status") == "FAILED" || platform.Text(team, "status") == "failed" {
				if _, e = s.call(ctx, "order", "group_failed", id, user, nil); e != nil {
					return nil, e
				}
				if _, e = s.call(ctx, "payment", "close", id, user, nil); e != nil {
					return nil, e
				}
				a.State = "CANCELLING"
				if e = save(); e != nil {
					return nil, e
				}
				e = s.releaseResources(ctx, &a, q, steps, save)
				return platform.Result{"order_id": id, "status": a.State}, e
			}
			formed := platform.Text(team, "status") == "FORMED" || platform.Text(team, "status") == "formed"
			if b, _ := team["formed"].(bool); b {
				formed = true
			}
			if !formed {
				a.State = "PAID_WAIT_GROUP"
				if _, e = s.call(ctx, "order", "wait_group", id, user, nil); e != nil {
					return nil, e
				}
				if e = save(); e != nil {
					return nil, e
				}
				return platform.Result{"order_id": id, "status": a.State}, nil
			}
			if _, e = s.call(ctx, "order", "group_formed", id, user, nil); e != nil {
				return nil, e
			}
		}
		if steps["inventory"] != "CONFIRMED" {
			if _, err = s.call(ctx, "inventory", "confirm", id, user, nil); err != nil {
				return nil, err
			}
			steps["inventory"] = "CONFIRMED"
			if err = save(); err != nil {
				return nil, err
			}
		}
		if _, err = s.call(ctx, "order", "ready", id, user, nil); err != nil {
			return nil, err
		}
		a.State = "READY_TO_FULFILL"
		if err = save(); err != nil {
			return nil, err
		}
		return platform.Result{"order_id": id, "status": a.State}, nil
	}
	return nil, status.Error(codes.InvalidArgument, "invalid workflow mode")
}
func (s *Service) releaseResources(ctx context.Context, a *domain.Saga, q map[string]any, steps map[string]string, save func() error) error {
	for _, resource := range []string{"points", "coupon", "group", "campaign", "inventory"} {
		st := steps[resource]
		if st == "" || st == "CANCELLED" {
			continue
		}
		if st == "CONFIRMED" && resource != "inventory" {
			continue
		}
		if resource == "inventory" && st == "CONFIRMED" {
			if _, err := s.call(ctx, "fulfillment", "cancel", a.ID, a.UserID, nil); err != nil {
				return err
			}
		}
		if _, err := s.call(ctx, resource, "cancel", a.ID, a.UserID, nil); err != nil {
			if resource != "group" || status.Code(err) != codes.FailedPrecondition {
				return err
			}
		}
		steps[resource] = "CANCELLED"
		if err := save(); err != nil {
			return err
		}
	}
	a.State = "CANCELLED"
	return save()
}
func (s *Service) cancel(ctx context.Context, c platform.Command) (platform.Result, error) {
	o, err := s.rpc.Call(ctx, "order", platform.Command{Action: "get", ID: c.ID, UserID: c.UserID, Role: c.Role})
	if err != nil {
		return nil, err
	}
	user := platform.Text(o, "user_id")
	st := platform.Text(o, "status")
	if st == "CLOSED" || st == "CREATE_FAILED" {
		return s.run(ctx, c.ID, user, "release")
	}
	if _, err = s.call(ctx, "order", "close_begin", c.ID, user, nil); err != nil {
		return nil, err
	}
	p, err := s.call(ctx, "payment", "close", c.ID, user, nil)
	if err != nil {
		return nil, err
	}
	paid, _ := p["paid"].(bool)
	if paid {
		p["payment_id"] = p["id"]
		accept, e := s.call(ctx, "order", "accept_payment", c.ID, user, p)
		if e != nil {
			err = e
			return nil, err
		}
		if accepted, _ := accept["accepted"].(bool); !accepted {
			return s.run(ctx, c.ID, user, "release")
		}
		return s.run(ctx, c.ID, user, "paid")
	}
	if _, err = s.call(ctx, "order", "close_complete", c.ID, user, nil); err != nil {
		return nil, err
	}
	return s.run(ctx, c.ID, user, "release")
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) (traceErr error) {
	ctx, sp := platform.Start(ctx, "checkout.usecase.OnEvent."+e.Type)
	defer sp.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	if e.Type == "OrderPaymentAccepted" {
		id := platform.Text(e.Data, "order_id")
		if id == "" {
			id = e.AggregateID
		}
		_, err := s.run(ctx, id, e.UserID, "paid")
		return err
	}
	if e.Type == "OrderClosed" {
		id := platform.Text(e.Data, "order_id")
		if id == "" {
			id = e.AggregateID
		}
		_, err := s.run(ctx, id, e.UserID, "release")
		return err
	}
	if e.Type == "GroupFormed" || e.Type == "GroupFailed" {
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
			if e.Type == "GroupFormed" {
				if _, err = s.call(ctx, "order", "group_formed", id, a.UserID, nil); err != nil {
					return err
				}
				if _, err = s.run(ctx, id, a.UserID, "paid"); err != nil {
					return err
				}
			} else {
				if _, err = s.call(ctx, "order", "group_failed", id, a.UserID, nil); err != nil {
					return err
				}
				if _, err = s.call(ctx, "payment", "close", id, a.UserID, nil); err != nil {
					return err
				}
				if _, err = s.run(ctx, id, a.UserID, "release"); err != nil {
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
		mode := "create"
		switch a.State {
		case "CANCELLING":
			mode = "release"
		case "PAID_CONFIRMING", "PAID_WAIT_GROUP":
			mode = "paid"
		}
		if _, e := s.run(ctx, a.ID, a.UserID, mode); e != nil && status.Code(e) != codes.Aborted && status.Code(e) != codes.FailedPrecondition {
			return e
		}
	}
	return nil
}
