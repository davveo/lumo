package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/davveo/lumo/internal/constants"
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

func (s *Service) Topics() []string {
	return []string{
		constants.EventAftersaleRecoveryRequested,
		constants.EventRefundSucceeded,
		constants.EventDeliveryConfirmed,
	}
}
func staff(c platform.Request) bool {
	return c.Role == constants.RoleAdmin || c.Role == constants.RoleSystem
}
func owner(c platform.Request, r *domain.Record) error {
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
	if e.Type == constants.EventAftersaleRecoveryRequested {
		return s.recoverOne(ctx, e)
	}
	switch e.Type {
	case constants.EventRefundSucceeded:
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
		_, err = s.Repo.Transition(
			ctx,
			id,
			[]string{constants.AftersaleStatusRefundRequested, constants.AftersaleStatusRefunding},
			constants.AftersaleStatusCompleted,
			map[string]any{"refund_id": platform.Text(e.Data, "refund_id")},
			nil,
		)
		return err
	case constants.EventDeliveryConfirmed:
		if platform.Text(e.Data, "kind") != constants.FulfillmentKindExchange {
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
		_, err = s.Repo.Transition(
			ctx,
			r.ID,
			[]string{constants.AftersaleStatusExchanging},
			constants.AftersaleStatusCompleted,
			nil,
			nil,
		)
		return err
	}
	return nil
}
func (s *Service) Approve(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.approve")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}

	if !staff(c) {
		return nil, status.Error(codes.PermissionDenied, "aftersale approval permission required")
	}
	if r.Type == constants.AftersaleTypeRefund {
		return s.approveRefund(ctx, r)
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusRequested},
		constants.AftersaleStatusApproved,
		nil,
		nil,
	)

	return platform.Map(r), err
}

func (s *Service) Cancel(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.cancel")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}

	if r.Type == constants.AftersaleTypeRefund {
		return s.releaseRefund(ctx, r, false)
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusRequested, constants.AftersaleStatusApproved},
		constants.AftersaleStatusCancelled,
		nil,
		nil,
	)

	return platform.Map(r), err
}

func (s *Service) Create(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.create")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	return s.create(ctx, c)
}

func (s *Service) Exchange(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.exchange")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}

	return s.exchange(ctx, r)
}

func (s *Service) Get(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.get")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}

	return platform.Map(r), nil
}

func (s *Service) Inspect(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.inspect")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}

	if !staff(c) {
		return nil, status.Error(codes.PermissionDenied, "warehouse inspection permission required")
	}
	return s.inspect(ctx, c, r)
}

func (s *Service) List(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.list")
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
	if staff(c) {
		user = ""
	}
	rows, page, err := s.Repo.ListPage(ctx, user, c)
	if err != nil {
		return nil, err
	}
	page["items"] = rows
	return page, nil
}

func (s *Service) Refund(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.refund")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}

	return s.refund(ctx, r)
}

func (s *Service) Reject(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.reject")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}

	if !staff(c) {
		return nil, status.Error(codes.PermissionDenied, "aftersale approval permission required")
	}
	if r.Type == constants.AftersaleTypeRefund {
		return s.releaseRefund(ctx, r, true)
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusRequested},
		constants.AftersaleStatusRejected,
		nil,
		nil,
	)

	return platform.Map(r), err
}

func (s *Service) Return(ctx context.Context, c platform.Request) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "usecase.aftersale.return")
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	if err := platform.RequireUser(c); err != nil {
		return nil, err
	}

	r, err := s.Repo.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if err = owner(c, r); err != nil {
		return nil, err
	}

	if r.Type == constants.AftersaleTypeRefund {
		return nil, status.Error(codes.FailedPrecondition, "refund-only requires no physical return")
	}
	tracking := strings.TrimSpace(platform.Text(c.Data, "tracking"))
	if tracking == "" || len(tracking) > 100 {
		return nil, status.Error(codes.InvalidArgument, "return tracking required")
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusApproved},
		constants.AftersaleStatusReturning,
		map[string]any{"return_tracking": tracking},
		nil,
	)

	return platform.Map(r), err
}

type orderLine struct {
	SKUID          string `json:"sku_id"`
	Quantity       int64  `json:"quantity"`
	PriceCents     int64  `json:"price_cents"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	LineTotalCents int64  `json:"line_total_cents"`
}

func (s *Service) create(ctx context.Context, c platform.Request) (platform.Result, error) {
	orderID := platform.Text(c.Data, "order_id")
	typ := strings.ToLower(platform.Text(c.Data, "type"))
	if typ == constants.AftersaleTypeReturnRefund {
		typ = constants.AftersaleTypeReturn
	}
	if typ == constants.AftersaleTypeRefundOnly {
		typ = constants.AftersaleTypeRefund
	}
	if c.IdempotencyKey == "" || orderID == "" ||
		(typ != constants.AftersaleTypeRefund && typ != constants.AftersaleTypeReturn &&
			typ != constants.AftersaleTypeExchange) {
		return nil, status.Error(codes.InvalidArgument, "idempotency key, order_id and valid type required")
	}
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if reason == "" || len(reason) > 1000 {
		return nil, status.Error(codes.InvalidArgument, "valid aftersale reason required")
	}
	o, err := platform.NewClients(s.RPC).Order.Get(
		ctx, platform.Request{ID: orderID, UserID: c.UserID, Role: c.Role},
	)
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
	prepared, err := prepareClaim(c, o, typ)
	if err != nil {
		return nil, err
	}
	purchased, weights, requested, amount, payable := prepared.Purchased, prepared.Weights, prepared.Requested, prepared.Amount, prepared.Payable

	sum := sha256.Sum256([]byte(user + "\x00" + c.IdempotencyKey))
	id, err := s.Repo.BusinessID(ctx, "aftersale", user+"\x00"+c.IdempotencyKey, fmt.Sprintf("as-%x", sum[:16]))
	if err != nil {
		return nil, err
	}
	lines, _ := json.Marshal(requested)
	row := &domain.Record{
		ID:             id,
		UserID:         user,
		OrderID:        orderID,
		PaymentID:      platform.Text(o, "payment_id"),
		Type:           typ,
		Reason:         reason,
		Status:         constants.AftersaleStatusRequested,
		LinesJSON:      string(lines),
		AmountCents:    amount,
		ReplacementSKU: platform.Text(c.Data, "replacement_sku_id"),
	}
	if row.ReplacementSKU != "" && len(requested) != 1 {
		return nil, status.Error(codes.InvalidArgument, "replacement SKU requires a single original SKU")
	}
	if row.ReplacementSKU != "" && row.ReplacementSKU != requested[0].SKUID {
		target, callErr := platform.NewClients(s.RPC).Catalog.Get(
			ctx, platform.Request{
				ID:     row.ReplacementSKU,
				UserID: user,
				Role:   constants.RoleSystem,
			},
		)
		if callErr != nil {
			return nil, callErr
		}
		if active, ok := target["active"].(bool); ok && !active {
			return nil, status.Error(codes.FailedPrecondition, "replacement SKU is inactive")
		}
		if platform.Int(target, "price_cents") != weights[requested[0].SKUID]/purchased[requested[0].SKUID] {
			return nil, status.Error(
				codes.FailedPrecondition,
				"price-changing exchange requires a differential-payment workflow; this provider supports equal-price exchange",
			)
		}
	}
	var attachments struct {
		IDs []string `json:"evidence_ids"`
	}
	if err = platform.Decode(c.Data, &attachments); err != nil {
		return nil, err
	}
	if len(attachments.IDs) > 5 {
		return nil, status.Error(codes.InvalidArgument, "最多提供 5 张售后图片")
	}
	if len(attachments.IDs) > 0 {
		files, e := platform.NewClients(
			s.RPC,
		).Media.VerifyFiles(
			ctx,
			platform.Request{UserID: user, Role: constants.RoleSystem, Data: map[string]any{"ids": attachments.IDs}},
		)
		if e != nil {
			return nil, e
		}
		var verified struct {
			Items []platform.Result `json:"items"`
		}
		if e = platform.Decode(files, &verified); e != nil {
			return nil, e
		}
		if len(verified.Items) != len(attachments.IDs) {
			return nil, status.Error(codes.FailedPrecondition, "图片凭证校验不完整")
		}
		sort.Slice(verified.Items, func(i, j int) bool {
			return platform.Text(verified.Items[i], "file_id") < platform.Text(verified.Items[j], "file_id")
		})
		raw, _ := json.Marshal(verified.Items)
		row.EvidenceJSON = string(raw)
	}
	policy, e := s.Repo.Policy(ctx)
	if e != nil {
		return nil, e
	}
	rawPolicy, _ := json.Marshal(policy)
	row.PolicyJSON = string(rawPolicy)
	if err = s.Repo.Claim(ctx, row, purchased, payable, stageLimits(o, purchased, typ)); err != nil {
		return nil, err
	}
	return platform.Map(row), nil
}
func (s *Service) inspect(ctx context.Context, c platform.Request, r *domain.Record) (platform.Result, error) {
	condition := strings.ToLower(platform.Text(c.Data, "condition"))
	if condition != constants.StockConditionSellable && condition != constants.StockConditionDamaged &&
		condition != constants.StockConditionUnsellable {
		return nil, status.Error(codes.InvalidArgument, "inspection condition required")
	}
	if r.Status == constants.AftersaleStatusInspected {
		return platform.Map(r), nil
	}
	if r.Status == constants.AftersaleStatusInspecting && r.Condition != condition {
		return nil, status.Error(codes.AlreadyExists, "inspection condition cannot change after receipt starts")
	}
	if r.Type == constants.AftersaleTypeRefund {
		return nil, status.Error(codes.FailedPrecondition, "refund-only has no return inspection")
	}
	r, err := s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusReturning},
		constants.AftersaleStatusInspecting,
		map[string]any{"condition": condition},
		nil,
	)
	if err != nil {
		return nil, err
	}
	var lines []domain.Line
	if err = json.Unmarshal([]byte(r.LinesJSON), &lines); err != nil {
		return nil, err
	}
	receipts := []map[string]any{}
	for _, line := range lines {
		digest := sha256.Sum256([]byte(r.ID + ":" + line.SKUID))
		receipts = append(
			receipts,
			map[string]any{
				"id":        fmt.Sprintf("inspection-%x", digest[:16]),
				"order_id":  r.OrderID,
				"sku_id":    line.SKUID,
				"quantity":  line.Quantity,
				"condition": r.Condition,
			},
		)
	}
	if _, err = platform.NewClients(s.RPC).Fulfillment.InspectReturns(ctx, platform.Request{UserID: r.UserID, Role: constants.RoleSystem, Data: map[string]any{"items": receipts}}); err != nil {
		return nil, err
	}

	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusInspecting},
		constants.AftersaleStatusInspected,
		nil,
		nil,
	)
	return platform.Map(r), err
}
func (s *Service) refund(ctx context.Context, r *domain.Record) (platform.Result, error) {
	if r.Type == constants.AftersaleTypeExchange {
		return nil, status.Error(codes.FailedPrecondition, "exchange cannot directly execute a refund")
	}
	if r.Status == constants.AftersaleStatusCompleted || r.Status == constants.AftersaleStatusRefunding {
		return platform.Map(r), nil
	}
	allowed := []string{constants.AftersaleStatusInspected}
	if r.Type == constants.AftersaleTypeRefund {
		allowed = []string{constants.AftersaleStatusApproved}
	}
	r, err := s.Repo.Transition(ctx, r.ID, allowed, constants.AftersaleStatusRefundRequested, nil, nil)
	if err != nil {
		return nil, err
	}
	if r.Type == constants.AftersaleTypeRefund {
		// Older approved applications may not yet have a warehouse line hold.
		if _, err = platform.NewClients(s.RPC).Fulfillment.HoldLines(ctx, holdRequest(r)); err != nil {
			if status.Code(err) == codes.FailedPrecondition {
				_, _ = s.Repo.Transition(
					ctx,
					r.ID,
					[]string{constants.AftersaleStatusRefundRequested},
					constants.AftersaleStatusApproved,
					nil,
					nil,
				)
			}
			return nil, err
		}
		confirmation, callErr := platform.NewClients(s.RPC).Fulfillment.CancelLines(ctx, holdRequest(r))
		if callErr != nil {
			return nil, callErr
		}
		if yes, _ := confirmation["warehouse_confirmed"].(bool); !yes {
			return nil, status.Error(codes.FailedPrecondition, "等待仓库行取消确认")
		}
	}
	refundID := "refund:" + r.ID
	refundResult, err := platform.NewClients(s.RPC).Refund.Create(
		ctx, platform.Request{
			ID:             refundID,
			UserID:         r.UserID,
			Role:           constants.RoleSystem,
			IdempotencyKey: refundID,
			Data: map[string]any{
				"order_id":     r.OrderID,
				"payment_id":   r.PaymentID,
				"amount_cents": r.AmountCents,
				"reason":       r.Reason,
				"aftersale_id": r.ID,
			},
		},
	)
	if err != nil {
		return nil, err
	}
	if actual := platform.Text(refundResult, "id"); actual != "" {
		refundID = actual
	}
	if latest, readErr := s.Repo.Get(ctx, r.ID); readErr == nil && latest.Status == constants.AftersaleStatusCompleted {
		return platform.Map(latest), nil
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusRefundRequested},
		constants.AftersaleStatusRefunding,
		map[string]any{"refund_id": refundID},
		nil,
	)
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
	if r.Type != constants.AftersaleTypeExchange {
		return nil, status.Error(codes.FailedPrecondition, "not an exchange application")
	}
	if r.Status == constants.AftersaleStatusExchanging || r.Status == constants.AftersaleStatusCompleted {
		return platform.Map(r), nil
	}
	r, err := s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusInspected},
		constants.AftersaleStatusExchangePreparing,
		map[string]any{"replacement_id": r.ID + ":exchange"},
		nil,
	)
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
	command := platform.Request{
		ID:     id,
		UserID: r.UserID,
		Role:   constants.RoleSystem,
		Data:   map[string]any{"order_id": r.OrderID, "lines": lines},
	}
	if _, err = platform.NewClients(s.RPC).Inventory.Reserve(ctx, command); err != nil {
		return nil, err
	}
	command.Data = nil
	if _, err = platform.NewClients(s.RPC).Inventory.Confirm(ctx, command); err != nil {
		return nil, err
	}
	if _, err = platform.NewClients(s.RPC).Fulfillment.CreateExchange(
		ctx, platform.Request{
			ID:     id,
			UserID: r.UserID,
			Role:   constants.RoleSystem,
			Data:   map[string]any{"order_id": r.OrderID, "reservation_id": id, "lines": lines},
		},
	); err != nil {
		return nil, err
	}
	r, err = s.Repo.Transition(
		ctx,
		r.ID,
		[]string{constants.AftersaleStatusExchangePreparing},
		constants.AftersaleStatusExchanging,
		map[string]any{"replacement_id": id},
		nil,
	)
	return platform.Map(r), err
}
