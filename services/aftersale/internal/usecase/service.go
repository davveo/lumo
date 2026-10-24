package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/domain"
	"github.com/davveo/lumo/services/aftersale/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service struct {
	Repo *repository.Repository
	RPC  platform.Caller
}

func (s *Service) Topics() []string { return []string{"RefundSucceeded", "DeliveryConfirmed"} }
func staff(c platform.Command) bool { return c.Role == "admin" || c.Role == "system" }
func owner(c platform.Command, r *domain.Record) error {
	if !staff(c) && (c.UserID == "" || c.UserID != r.UserID) {
		return status.Error(codes.PermissionDenied, "aftersale ownership mismatch")
	}
	return nil
}
func (s *Service) OnEvent(ctx context.Context, e platform.Event) (traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.event."+e.Type)
	defer span.End()
	platform.TraceRequest(ctx, e)
	defer func() { platform.TraceResponse(ctx, nil, traceErr) }()
	switch e.Type {
	case "RefundSucceeded":
		id := platform.Text(e.Data, "aftersale_id")
		if id == "" {
			return nil
		}
		r, err := s.Repo.Get(ctx, id)
		if status.Code(err) == codes.NotFound {
			return nil
		}
		if err != nil {
			return err
		}
		if e.UserID != "" && r.UserID != e.UserID {
			return status.Error(codes.PermissionDenied, "refund event owner mismatch")
		}
		_, err = s.Repo.Transition(ctx, id, []string{"REFUND_REQUESTED", "REFUNDING"}, "COMPLETED", map[string]any{"refund_id": platform.Text(e.Data, "refund_id")}, nil)
		return err
	case "DeliveryConfirmed":
		if platform.Text(e.Data, "kind") != "EXCHANGE" {
			return nil
		}
		id := platform.Text(e.Data, "fulfillment_id")
		r, err := s.Repo.ByReplacement(ctx, id)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return nil
			}
			return err
		}
		_, err = s.Repo.Transition(ctx, r.ID, []string{"EXCHANGING"}, "COMPLETED", nil, nil)
		return err
	}
	return nil
}
func (s *Service) Handle(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}
	if c.Action == "list" {
		user := c.UserID
		if staff(c) {
			user = ""
		}
		items, err := s.Repo.List(ctx, user)
		return platform.Result{"items": items}, err
	}
	if c.Action == "create" {
		return s.create(ctx, c)
	}
	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}
	switch c.Action {
	case "get":
		return platform.Map(r), nil
	case "approve":
		if !staff(c) {
			return nil, status.Error(codes.PermissionDenied, "aftersale approval permission required")
		}
		r, err = s.Repo.Transition(ctx, r.ID, []string{"REQUESTED"}, "APPROVED", nil, nil)
	case "reject":
		if !staff(c) {
			return nil, status.Error(codes.PermissionDenied, "aftersale approval permission required")
		}
		r, err = s.Repo.Transition(ctx, r.ID, []string{"REQUESTED"}, "REJECTED", nil, nil)
	case "cancel":
		r, err = s.Repo.Transition(ctx, r.ID, []string{"REQUESTED", "APPROVED"}, "CANCELLED", nil, nil)
	case "return":
		if r.Type == "refund" {
			return nil, status.Error(codes.FailedPrecondition, "refund-only requires no physical return")
		}
		tracking := strings.TrimSpace(platform.Text(c.Data, "tracking"))
		if tracking == "" || len(tracking) > 100 {
			return nil, status.Error(codes.InvalidArgument, "return tracking required")
		}
		r, err = s.Repo.Transition(ctx, r.ID, []string{"APPROVED"}, "RETURNING", map[string]any{"return_tracking": tracking}, nil)
	case "inspect":
		if !staff(c) {
			return nil, status.Error(codes.PermissionDenied, "warehouse inspection permission required")
		}
		return s.inspect(ctx, c, r)
	case "refund":
		return s.refund(ctx, r)
	case "exchange":
		return s.exchange(ctx, r)
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported aftersale action")
	}
	return platform.Map(r), err
}

type orderLine struct {
	SKUID          string `json:"sku_id"`
	Quantity       int64  `json:"quantity"`
	PriceCents     int64  `json:"price_cents"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	LineTotalCents int64  `json:"line_total_cents"`
}

func (s *Service) create(ctx context.Context, c platform.Command) (platform.Result, error) {
	orderID := platform.Text(c.Data, "order_id")
	typ := strings.ToLower(platform.Text(c.Data, "type"))
	if typ == "return_refund" {
		typ = "return"
	}
	if typ == "refund_only" {
		typ = "refund"
	}
	if c.IdempotencyKey == "" || orderID == "" || (typ != "refund" && typ != "return" && typ != "exchange") {
		return nil, status.Error(codes.InvalidArgument, "idempotency key, order_id and valid type required")
	}
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if reason == "" || len(reason) > 1000 {
		return nil, status.Error(codes.InvalidArgument, "valid aftersale reason required")
	}
	o, err := s.RPC.Call(ctx, "order", platform.Command{Action: "get", ID: orderID, UserID: c.UserID, Role: c.Role})
	if err != nil {
		return nil, err
	}
	user := platform.Text(o, "user_id")
	if user == "" {
		return nil, status.Error(codes.FailedPrecondition, "order owner unavailable")
	}
	if user != c.UserID && !staff(c) {
		return nil, status.Error(codes.PermissionDenied, "order ownership mismatch")
	}
	state := platform.Text(o, "status")
	if state != "READY_TO_FULFILL" && state != "SHIPPED" && state != "COMPLETED" {
		return nil, status.Error(codes.FailedPrecondition, "order is not eligible for aftersale")
	}
	if typ == "refund" && (state == "SHIPPED" || state == "COMPLETED") {
		return nil, status.Error(codes.FailedPrecondition, "shipped order requires physical return or staff-reviewed return policy")
	}
	var items []orderLine
	b, _ := json.Marshal(o["items"])
	if err = json.Unmarshal(b, &items); err != nil || len(items) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "order lines unavailable")
	}
	purchased := map[string]int64{}
	weights := map[string]int64{}
	var base int64
	for _, i := range items {
		if i.SKUID == "" || i.Quantity <= 0 {
			return nil, status.Error(codes.FailedPrecondition, "invalid order snapshot")
		}
		purchased[i.SKUID] += i.Quantity
		unit := i.PriceCents
		if unit == 0 {
			unit = i.UnitPriceCents
		}
		line := unit * i.Quantity
		if i.LineTotalCents > 0 {
			line = i.LineTotalCents
		}
		if line <= 0 {
			line = i.Quantity
		}
		weights[i.SKUID] += line
		base += line
	}
	var requested []domain.Line
	if c.Data["lines"] != nil {
		b, _ = json.Marshal(c.Data["lines"])
		if err = json.Unmarshal(b, &requested); err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid aftersale lines")
		}
	} else {
		for k, q := range purchased {
			requested = append(requested, domain.Line{SKUID: k, Quantity: q})
		}
	}
	if len(requested) == 0 {
		return nil, status.Error(codes.InvalidArgument, "aftersale lines required")
	}
	normalized := map[string]int64{}
	for _, i := range requested {
		if i.Quantity <= 0 || purchased[i.SKUID] == 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid requested quantity")
		}
		normalized[i.SKUID] += i.Quantity
	}
	requested = nil
	for k, q := range normalized {
		if q > purchased[k] {
			return nil, status.Error(codes.FailedPrecondition, "quantity exceeds original purchase")
		}
		requested = append(requested, domain.Line{SKUID: k, Quantity: q})
	}
	if typ == "refund" && !wholeOrder(normalized, purchased) {
		return nil, status.Error(codes.FailedPrecondition, "partial refund before shipment requires partial warehouse cancellation; only whole-order refund is supported")
	}
	sort.Slice(requested, func(i, j int) bool { return requested[i].SKUID < requested[j].SKUID })
	payable := platform.Int(o, "payable_cents")
	var weight int64
	for _, l := range requested {
		weight += weights[l.SKUID] * l.Quantity / purchased[l.SKUID]
	}
	amount := payable * weight / base
	if typ == "exchange" {
		amount = 0
	}
	if typ != "exchange" && amount <= 0 {
		return nil, status.Error(codes.FailedPrecondition, "no refundable cash for requested lines")
	}
	sum := sha256.Sum256([]byte(user + "\x00" + c.IdempotencyKey))
	id := fmt.Sprintf("as-%x", sum[:16])
	lines, _ := json.Marshal(requested)
	row := &domain.Record{ID: id, UserID: user, OrderID: orderID, PaymentID: platform.Text(o, "payment_id"), Type: typ, Reason: reason, Status: "REQUESTED", LinesJSON: string(lines), AmountCents: amount, ReplacementSKU: platform.Text(c.Data, "replacement_sku_id")}
	if row.ReplacementSKU != "" && len(requested) != 1 {
		return nil, status.Error(codes.InvalidArgument, "replacement SKU requires a single original SKU")
	}
	if row.ReplacementSKU != "" && row.ReplacementSKU != requested[0].SKUID {
		target, callErr := s.RPC.Call(ctx, "catalog", platform.Command{Action: "get", ID: row.ReplacementSKU, UserID: user, Role: "system"})
		if callErr != nil {
			return nil, callErr
		}
		if active, ok := target["active"].(bool); ok && !active {
			return nil, status.Error(codes.FailedPrecondition, "replacement SKU is inactive")
		}
		if platform.Int(target, "price_cents") != weights[requested[0].SKUID]/purchased[requested[0].SKUID] {
			return nil, status.Error(codes.FailedPrecondition, "price-changing exchange requires a differential-payment workflow; this provider supports equal-price exchange")
		}
	}
	if err = s.Repo.Claim(ctx, row, purchased, payable); err != nil {
		return nil, err
	}
	return platform.Map(row), nil
}
func (s *Service) inspect(ctx context.Context, c platform.Command, r *domain.Record) (platform.Result, error) {
	condition := strings.ToLower(platform.Text(c.Data, "condition"))
	if condition != "sellable" && condition != "damaged" && condition != "unsellable" {
		return nil, status.Error(codes.InvalidArgument, "inspection condition required")
	}
	if r.Status == "INSPECTED" {
		return platform.Map(r), nil
	}
	if r.Status == "INSPECTING" && r.Condition != condition {
		return nil, status.Error(codes.AlreadyExists, "inspection condition cannot change after receipt starts")
	}
	if r.Type == "refund" {
		return nil, status.Error(codes.FailedPrecondition, "refund-only has no return inspection")
	}
	r, err := s.Repo.Transition(ctx, r.ID, []string{"RETURNING"}, "INSPECTING", map[string]any{"condition": condition}, nil)
	if err != nil {
		return nil, err
	}
	var lines []domain.Line
	if err = json.Unmarshal([]byte(r.LinesJSON), &lines); err != nil {
		return nil, err
	}
	for _, line := range lines {
		digest := sha256.Sum256([]byte(r.ID + ":" + line.SKUID))
		receipt := fmt.Sprintf("inspection-%x", digest[:16])
		_, err = s.RPC.Call(ctx, "fulfillment", platform.Command{Action: "inspect_return", ID: receipt, UserID: r.UserID, Role: "system", Data: map[string]any{"order_id": r.OrderID, "sku_id": line.SKUID, "quantity": line.Quantity, "condition": r.Condition}})
		if err != nil {
			return nil, err
		}
	}
	r, err = s.Repo.Transition(ctx, r.ID, []string{"INSPECTING"}, "INSPECTED", nil, nil)
	return platform.Map(r), err
}
func (s *Service) refund(ctx context.Context, r *domain.Record) (platform.Result, error) {
	if r.Type == "exchange" {
		return nil, status.Error(codes.FailedPrecondition, "exchange cannot directly execute a refund")
	}
	if r.Status == "COMPLETED" || r.Status == "REFUNDING" {
		return platform.Map(r), nil
	}
	if r.Type == "refund" {
		// Revalidate stored applications too: earlier versions accepted partial
		// refund-only requests, while the warehouse cancellation is order-wide.
		o, err := s.RPC.Call(ctx, "order", platform.Command{Action: "get", ID: r.OrderID, UserID: r.UserID, Role: "system"})
		if err != nil {
			return nil, err
		}
		var items []orderLine
		encoded, _ := json.Marshal(o["items"])
		if err = json.Unmarshal(encoded, &items); err != nil || len(items) == 0 {
			return nil, status.Error(codes.FailedPrecondition, "order lines unavailable")
		}
		purchased := map[string]int64{}
		for _, item := range items {
			purchased[item.SKUID] += item.Quantity
		}
		var lines []domain.Line
		if err = json.Unmarshal([]byte(r.LinesJSON), &lines); err != nil {
			return nil, err
		}
		requested := map[string]int64{}
		for _, line := range lines {
			requested[line.SKUID] += line.Quantity
		}
		if !wholeOrder(requested, purchased) {
			return nil, status.Error(codes.FailedPrecondition, "partial refund before shipment requires partial warehouse cancellation; only whole-order refund is supported")
		}
	}
	allowed := []string{"INSPECTED"}
	if r.Type == "refund" {
		allowed = []string{"APPROVED"}
	}
	r, err := s.Repo.Transition(ctx, r.ID, allowed, "REFUND_REQUESTED", nil, nil)
	if err != nil {
		return nil, err
	}
	if r.Type == "refund" {
		if _, err = s.RPC.Call(ctx, "fulfillment", platform.Command{Action: "cancel", ID: r.OrderID, UserID: r.UserID, Role: "system"}); err != nil {
			return nil, err
		}
	}
	refundID := "refund:" + r.ID
	_, err = s.RPC.Call(ctx, "refund", platform.Command{Action: "create", ID: refundID, UserID: r.UserID, Role: "system", IdempotencyKey: refundID, Data: map[string]any{"order_id": r.OrderID, "payment_id": r.PaymentID, "amount_cents": r.AmountCents, "reason": r.Reason, "aftersale_id": r.ID}})
	if err != nil {
		return nil, err
	}
	if latest, readErr := s.Repo.Get(ctx, r.ID); readErr == nil && latest.Status == "COMPLETED" {
		return platform.Map(latest), nil
	}
	r, err = s.Repo.Transition(ctx, r.ID, []string{"REFUND_REQUESTED"}, "REFUNDING", map[string]any{"refund_id": refundID}, nil)
	return platform.Map(r), err
}

func wholeOrder(requested, purchased map[string]int64) bool {
	if len(requested) != len(purchased) || len(purchased) == 0 {
		return false
	}
	for sku, qty := range purchased {
		if qty <= 0 || requested[sku] != qty {
			return false
		}
	}
	return true
}

func (s *Service) exchange(ctx context.Context, r *domain.Record) (platform.Result, error) {
	if r.Type != "exchange" {
		return nil, status.Error(codes.FailedPrecondition, "not an exchange application")
	}
	if r.Status == "EXCHANGING" || r.Status == "COMPLETED" {
		return platform.Map(r), nil
	}
	r, err := s.Repo.Transition(ctx, r.ID, []string{"INSPECTED"}, "EXCHANGE_PREPARING", map[string]any{"replacement_id": r.ID + ":exchange"}, nil)
	if err != nil {
		return nil, err
	}
	var lines []domain.Line
	if err = json.Unmarshal([]byte(r.LinesJSON), &lines); err != nil {
		return nil, err
	}
	if r.ReplacementSKU != "" {
		lines[0].SKUID = r.ReplacementSKU
	}
	id := r.ID + ":exchange"
	command := platform.Command{Action: "reserve", ID: id, UserID: r.UserID, Role: "system", Data: map[string]any{"order_id": r.OrderID, "lines": lines}}
	if _, err = s.RPC.Call(ctx, "inventory", command); err != nil {
		return nil, err
	}
	command.Action = "confirm"
	command.Data = nil
	if _, err = s.RPC.Call(ctx, "inventory", command); err != nil {
		return nil, err
	}
	if _, err = s.RPC.Call(ctx, "fulfillment", platform.Command{Action: "create_exchange", ID: id, UserID: r.UserID, Role: "system", Data: map[string]any{"order_id": r.OrderID, "reservation_id": id, "lines": lines}}); err != nil {
		return nil, err
	}
	r, err = s.Repo.Transition(ctx, r.ID, []string{"EXCHANGE_PREPARING"}, "EXCHANGING", map[string]any{"replacement_id": id}, nil)
	return platform.Map(r), err
}
