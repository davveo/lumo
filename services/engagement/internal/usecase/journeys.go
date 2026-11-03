package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/engagement/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"time"
)

func (s *Service) JourneyRules(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.repo.JourneyRules(ctx, c)
}
func (s *Service) JourneyRuleSave(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.JourneyRuleSave")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var row domain.JourneyRule
	if e := platform.Decode(c.Data, &row); e != nil {
		return nil, e
	}
	row.ID = c.ID
	if row.ID == "" {
		if c.IdempotencyKey == "" {
			return nil, bad("规则请求标识必填")
		}
		row.ID = stable("journey-rule", c.UserID, c.IdempotencyKey)
	}
	row.Name = strings.TrimSpace(row.Name)
	if len(row.ID) > 128 || row.Name == "" || len([]rune(row.Name)) > 80 || row.DelayMinutes < 1 ||
		row.DelayMinutes > 10080 ||
		row.CycleDays < 1 ||
		row.CycleDays > 365 ||
		row.GapHours < 24 ||
		row.GapHours > 720 ||
		row.Version < 0 ||
		len(row.SKUID) > 128 ||
		len(row.SegmentID) > 128 {
		return nil, bad("请设置有效名称、等待1至10080分钟、周期1至365天和发送间隔24至720小时")
	}
	if row.Kind != constants.JourneyKindCart && row.Kind != constants.JourneyKindUnpaid &&
		row.Kind != constants.JourneyKindRepurchase {
		return nil, bad("不支持的触达类型")
	}
	clients := platform.NewClients(s.rpc)
	if row.Kind == constants.JourneyKindRepurchase {
		if row.SKUID == "" {
			return nil, bad("复购规则必须选择SKU")
		}
		if _, e := clients.Catalog.Get(ctx, platform.Request{ID: row.SKUID, UserID: c.UserID, Role: constants.RoleAdmin}); e != nil {
			return nil, e
		}
	} else {
		row.SKUID = ""
	}
	if row.SegmentID != "" {
		if _, e := clients.Member.SegmentMatch(ctx, platform.Request{ID: row.SegmentID, UserID: c.UserID, Role: constants.RoleSystem, Data: map[string]any{"validate_only": true}}); e != nil {
			return nil, e
		}
	}
	row.ActorID = c.UserID
	saved, e := s.repo.SaveJourneyRule(ctx, row)
	return platform.Map(saved), e
}
func (s *Service) JourneyList(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.repo.Journeys(ctx, c)
}
func (s *Service) JourneyCancel(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	e := s.repo.CancelJourney(ctx, c.ID)
	return platform.Result{"cancelled": e == nil}, e
}
func activityTime(value string) time.Time {
	at, e := time.Parse(time.RFC3339Nano, value)
	if e != nil {
		return time.Now().UTC()
	}
	return at.UTC()
}
func journeySeed(rule domain.JourneyRule, user, source string, at, expires time.Time) domain.Journey {
	due := at.Add(time.Duration(rule.DelayMinutes) * time.Minute)
	if rule.Kind == constants.JourneyKindRepurchase {
		due = at.AddDate(0, 0, int(rule.CycleDays))
		expires = due.Add(7 * 24 * time.Hour)
	}
	if expires.IsZero() {
		expires = at.Add(30 * 24 * time.Hour)
	}
	return domain.Journey{
		SlotKey:   stable("journey", user, rule.ID, source),
		UserID:    user,
		RuleID:    rule.ID,
		Kind:      rule.Kind,
		SourceID:  source,
		SKUID:     rule.SKUID,
		SourceAt:  at,
		DueAt:     due,
		ExpiresAt: expires,
	}
}

type journeyLine struct {
	SKU      string `json:"sku_id"`
	Quantity int64  `json:"quantity"`
}

func cartActivity(result platform.Result) ([]journeyLine, time.Time, error) {
	var rows struct {
		Items []map[string]any `json:"items"`
	}
	if e := platform.Decode(result, &rows); e != nil {
		return nil, time.Time{}, e
	}
	items := []journeyLine{}
	var at time.Time
	for _, r := range rows.Items {
		q := platform.Int(r, "quantity")
		sku := platform.Text(r, "sku_id")
		if q > 0 && sku != "" {
			items = append(items, journeyLine{SKU: sku, Quantity: q})
			v := activityTime(platform.Text(r, "updated_at"))
			if v.After(at) {
				at = v
			}
		}
	}
	return items, at, nil
}
func (s *Service) JourneyRun(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.JourneyRun")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	rule, e := s.repo.JourneyRule(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if !rule.Enabled {
		return nil, bad("请先启用规则")
	}
	user := platform.Text(c.Data, "customer_id")
	if user == "" || len(user) > 128 {
		return nil, bad("请选择已注册顾客")
	}
	clients := platform.NewClients(s.rpc)
	account, e := clients.IAM.AdminGet(ctx, platform.Request{ID: user, UserID: c.UserID, Role: constants.RoleAdmin})
	if e != nil {
		return nil, e
	}
	if platform.Text(account, "role") != constants.RoleUser || account["active"] == false {
		return nil, bad("仅支持正常的普通会员账号")
	}
	source := platform.Text(c.Data, "order_id")
	var at, expires time.Time
	if rule.Kind == constants.JourneyKindCart {
		cart, e := clients.Cart.List(
			ctx,
			platform.Request{UserID: user, Role: constants.RoleSystem, Data: map[string]any{"limit": 100}},
		)
		if e != nil {
			return nil, e
		}
		lines, last, e := cartActivity(cart)
		if e != nil {
			return nil, e
		}
		if len(lines) == 0 {
			return nil, bad("顾客当前购物车为空")
		}
		source = user
		at = last
	} else {
		if source == "" || len(source) > 128 {
			return nil, bad("请输入该顾客的订单编号")
		}
		order, e := clients.Order.Get(ctx, platform.Request{ID: source, UserID: user, Role: constants.RoleSystem})
		if e != nil {
			return nil, e
		}
		if platform.Text(order, "user_id") != user {
			return nil, status.Error(codes.PermissionDenied, "订单不属于所选顾客")
		}
		if rule.Kind == constants.JourneyKindUnpaid {
			if platform.Text(order, "status") != constants.OrderStatusPendingPayment {
				return nil, bad("仅支持待支付订单")
			}
			at = activityTime(platform.Text(order, "created_at"))
			expires = activityTime(platform.Text(order, "expires_at"))
		} else {
			if platform.Text(order, "status") != constants.OrderStatusCompleted || !orderHasSKU(order, rule.SKUID) {
				return nil, bad("复购提醒需要包含指定商品的已完成订单")
			}
			at = activityTime(platform.Text(order, "updated_at"))
		}
	}
	seed := journeySeed(rule, user, source, at, expires)
	e = s.repo.SeedJourneys(ctx, []domain.Journey{seed}, "")
	return platform.Result{"queued_for_check": e == nil, "due_at": seed.DueAt, "scope": "投递前再次检查营销通知偏好、当前业务状态与跨规则频控"}, e
}
func orderHasSKU(order platform.Result, sku string) bool {
 var progress struct{Lines []struct{SKU string `json:"sku_id"`;Quantity int64 `json:"quantity"`;Cancelled int64 `json:"cancelled"`} `json:"fulfillment_progress"`}
 _=platform.Decode(order,&progress);for _,l:=range progress.Lines{if l.SKU==sku&&l.Cancelled>=l.Quantity{return false}}

	var input struct {
		Items []journeyLine `json:"items"`
	}
	_ = platform.Decode(order, &input)
	for _, r := range input.Items {
		if r.SKU == sku && r.Quantity > 0 {
			return true
		}
	}
	return false
}
func (s *Service) event(ctx context.Context, event platform.Event) error {
	ctx, sp := platform.Start(ctx, "engagement.usecase.ProjectJourney")
	defer sp.End()
	if event.UserID == "" {
		return nil
	}
	if event.Type == constants.EventJourneyDelivered || event.Type == constants.EventJourneySuppressed {
		return s.repo.JourneyFeedback(
			ctx,
			event.AggregateID,
			event.UserID,
			platform.Int(event.Data, "generation"),
			event.Type == constants.EventJourneyDelivered,
		)
	}
	if event.Type == constants.EventOrderPaymentAccepted {
		var input struct {
			Items []journeyLine `json:"items"`
		}
		if e := platform.Decode(event.Data, &input); e != nil {
			return e
		}
		skus := map[string]bool{}
		for _, r := range input.Items {
			skus[r.SKU] = r.Quantity > 0
		}
		return s.repo.JourneyPayment(ctx, event, skus, activityTime(platform.Text(event.Data, "occurred_at")))
	}
	kind := constants.JourneyKindCart
	source := event.UserID
	at := activityTime(platform.Text(event.Data, "changed_at"))
	var expires time.Time
	var order platform.Result
	if event.Type == constants.EventOrderAwaitingPayment || event.Type == constants.EventOrderCompleted {
		var e error
		order, e = platform.NewClients(
			s.rpc,
		).Order.Get(
			ctx,
			platform.Request{ID: event.AggregateID, UserID: event.UserID, Role: constants.RoleSystem},
		)
		if e != nil {
			return e
		}
		if platform.Text(order, "user_id") != event.UserID {
			return status.Error(codes.PermissionDenied, "journey source owner mismatch")
		}
		source = event.AggregateID
		if event.Type == constants.EventOrderAwaitingPayment {
			if platform.Text(order, "status") != constants.OrderStatusPendingPayment {
				return nil
			}
			kind = constants.JourneyKindUnpaid
			at = activityTime(platform.Text(order, "created_at"))
			expires = activityTime(platform.Text(order, "expires_at"))
		} else {
			if platform.Text(order, "status") != constants.OrderStatusCompleted {
				return nil
			}
			kind = constants.JourneyKindRepurchase
			at = activityTime(platform.Text(order, "updated_at"))
		}
	} else if event.Type != constants.EventCartChanged {
		return nil
	}
	rules, e := s.repo.ActiveJourneyRules(ctx, kind)
	if e != nil {
		return e
	}
	seeds := []domain.Journey{}
	for _, rule := range rules {
		if kind == constants.JourneyKindRepurchase && !orderHasSKU(order, rule.SKUID) {
			continue
		}
		seeds = append(seeds, journeySeed(rule, event.UserID, source, at, expires))
	}
	return s.repo.SeedJourneys(ctx, seeds, event.ID)
}
func marketingAllowed(profile platform.Result) bool {
	var value struct {
		Preferences struct {
			Notifications struct {
				Promotion bool `json:"promotion"`
			} `json:"notifications"`
		} `json:"preferences"`
	}
	_ = platform.Decode(profile, &value)
	return value.Preferences.Notifications.Promotion
}

func (s *Service) journeyEligibility(
	ctx context.Context,
	j domain.Journey,
	rule domain.JourneyRule,
) ([]journeyLine, string, error) {
	if !time.Now().Before(j.ExpiresAt) {
		return nil, "触达已到期", nil
	}
	if !rule.Enabled || rule.Kind != j.Kind || rule.Kind == constants.JourneyKindRepurchase && rule.SKUID != j.SKUID {
		return nil, "规则已停用或范围已变化", nil
	}
	clients := platform.NewClients(s.rpc)
	account, e := clients.IAM.AdminGet(
		ctx,
		platform.Request{ID: j.UserID, UserID: constants.RoleSystem, Role: constants.RoleAdmin},
	)
	if e != nil {
		return nil, "", e
	}
	if platform.Text(account, "role") != constants.RoleUser || account["active"] == false {
		return nil, "顾客账号不可触达", nil
	}
	profile, e := clients.Member.Get(ctx, platform.Request{UserID: j.UserID, Role: constants.RoleSystem})
	if e != nil {
		return nil, "", e
	}
	if !marketingAllowed(profile) {
		return nil, "顾客未开启优惠活动通知", nil
	}
	if rule.SegmentID != "" {
		result, e := clients.Member.SegmentMatch(
			ctx,
			platform.Request{ID: rule.SegmentID, UserID: j.UserID, Role: constants.RoleSystem},
		)
		if e != nil {
			return nil, "", e
		}
		if result["matches"] != true {
			return nil, "当前不符合所选客户分群", nil
		}
	}
	if j.Kind == constants.JourneyKindCart {
		cart, e := clients.Cart.List(
			ctx,
			platform.Request{UserID: j.UserID, Role: constants.RoleSystem, Data: map[string]any{"limit": 100}},
		)
		if e != nil {
			return nil, "", e
		}
		lines, _, e := cartActivity(cart)
		if e != nil {
			return nil, "", e
		}
		if len(lines) == 0 {
			return nil, "购物车已清空", nil
		}
		return lines, "", nil
	}
	order, e := clients.Order.Get(ctx, platform.Request{ID: j.SourceID, UserID: j.UserID, Role: constants.RoleSystem})
	if e != nil {
		return nil, "", e
	}
	if platform.Text(order, "user_id") != j.UserID {
		return nil, "订单归属不匹配", nil
	}
	if j.Kind == constants.JourneyKindUnpaid {
		if platform.Text(order, "status") != constants.OrderStatusPendingPayment {
			return nil, "订单已支付或已关闭", nil
		}
		return nil, "", nil
	}
	if platform.Text(order, "status") != constants.OrderStatusCompleted || !orderHasSKU(order, j.SKUID) {
		return nil, "原商品已退款或原订单状态不适用", nil
	}
	p, e := clients.Catalog.Get(ctx, platform.Request{ID: j.SKUID, UserID: j.UserID, Role: constants.RoleSystem})
	if status.Code(e) == codes.NotFound {
		return nil, "复购商品已下架", nil
	}
	if e != nil {
		return nil, "", e
	}
	if p["active"] != true {
		return nil, "复购商品不可购买", nil
	}
	return []journeyLine{{SKU: j.SKUID, Quantity: 1}}, "", nil
}
func (s *Service) TickAll(ctx context.Context) error {
	if e := s.Tick(ctx); e != nil {
		return e
	}
	return s.tickJourney(ctx)
}
func (s *Service) tickJourney(ctx context.Context) error {
	ctx, sp := platform.Start(ctx, "engagement.usecase.TickJourney")
	defer sp.End()
	j, e := s.repo.ClaimJourney(ctx)
	if e != nil || j == nil {
		return e
	}
	rule, e := s.repo.JourneyRule(ctx, j.RuleID)
	if e != nil {
		return s.repo.QueueJourney(ctx, *j, rule, nil, "触达规则不存在", false)
	}
	lines, reason, e := s.journeyEligibility(ctx, *j, rule)
	if e != nil {
		return s.repo.QueueJourney(ctx, *j, rule, nil, "业务数据暂不可读取，5分钟后重试", true)
	}
	skus := []string{}
	for _, line := range lines {
		skus = append(skus, line.SKU)
	}
	return s.repo.QueueJourney(ctx, *j, rule, skus, reason, false)
}
func (s *Service) JourneyDeliveryCheck(ctx context.Context, c platform.Request) (platform.Result, error) {
	if c.Role != constants.RoleSystem {
		return nil, status.Error(codes.PermissionDenied, "internal delivery check required")
	}
	j, e := s.repo.Journey(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if j.UserID != c.UserID || j.Generation != platform.Int(c.Data, "generation") ||
		j.Status != constants.JourneyStatusQueued {
		return platform.Result{"eligible": false}, nil
	}
	rule, e := s.repo.JourneyRule(ctx, j.RuleID)
	if e != nil {
		return nil, e
	}
	_, reason, e := s.journeyEligibility(ctx, j, rule)
	return platform.Result{"eligible": reason == "" && e == nil}, e
}
func (s *Service) JourneyOpen(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "engagement.usecase.JourneyOpen")
	defer sp.End()
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	j, e := s.repo.Journey(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if j.UserID != c.UserID {
		return nil, status.Error(codes.PermissionDenied, "触达不属于此账号")
	}
	if !time.Now().Before(j.ExpiresAt) ||
		(j.Status != constants.JourneyStatusDelivered && j.Status != constants.JourneyStatusQueued) {
		return nil, status.Error(codes.FailedPrecondition, "此提醒已退出或已到期，可直接查看购物车或订单")
	}
	clients := platform.NewClients(s.rpc)
	target := "#/member/orders"
	warnings := []string{}
	lines := []journeyLine{}
	if j.Kind == constants.JourneyKindUnpaid {
		order, e := clients.Order.Get(ctx, platform.Request{ID: j.SourceID, UserID: c.UserID, Role: c.Role})
		if e != nil {
			return nil, e
		}
		if platform.Text(order, "status") != constants.OrderStatusPendingPayment {
			return nil, status.Error(codes.FailedPrecondition, "订单已支付或已关闭，请查看订单列表")
		}
	}
	if j.Kind == constants.JourneyKindCart {
		cart, e := clients.Cart.List(
			ctx,
			platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"limit": 100}},
		)
		if e != nil {
			return nil, e
		}
		lines, _, e = cartActivity(cart)
		if e != nil {
			return nil, e
		}
		if len(lines) == 0 {
			return nil, bad("当前购物车为空")
		}
		target = "#/cart"
	}
	if j.Kind == constants.JourneyKindRepurchase {
		p, e := clients.Catalog.Get(ctx, platform.Request{ID: j.SKUID, UserID: c.UserID, Role: c.Role})
		if e != nil {
			return nil, e
		}
		id := platform.Text(p, "product_id")
		if id == "" {
			id = j.SKUID
		}
		target = "#/product/" + id
		lines = []journeyLine{{SKU: j.SKUID, Quantity: 1}}
	}
	if len(lines) > 0 {
		ids := []string{}
		for _, line := range lines {
			ids = append(ids, line.SKU)
		}
		stocks, e := clients.Inventory.BatchGet(
			ctx,
			platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"ids": ids}},
		)
		if e != nil {
			return nil, e
		}
		stockIndex, e := platform.IndexResults(stocks, "sku_id")
		if e != nil {
			return nil, e
		}
		for _, line := range lines {
			if platform.Int(stockIndex[line.SKU], "available") < line.Quantity {
				warnings = append(warnings, "部分商品库存不足，请调整数量后结算")
				break
			}
		}
		if _, e = clients.Pricing.Optimize(ctx, platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"items": lines, "coupon_mode": "auto"}}); e != nil {
			warnings = append(warnings, "商品价格或优惠发生变化，请在结算页重新确认")
		}
	}
	if e = s.repo.JourneyClicked(ctx, j); e != nil {
		return nil, e
	}
	return platform.Result{"target": target, "warnings": warnings, "scope": "已按当前价格、优惠和库存重新检查，实际以新结算报价为准"}, nil
}
