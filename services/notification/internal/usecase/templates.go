package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/notification/internal/repository"
	"regexp"
	"strconv"
	"strings"
)

var notificationTitles = map[string]string{
	constants.EventProductPriceReached: "商品降价提醒", constants.EventProductBackInStock: "商品到货提醒",
	constants.EventOrderAwaitingPayment: "订单待支付", constants.EventOrderPaymentAccepted: "订单已收款", constants.EventOrderClosed: "订单已关闭", constants.EventGroupFormed: "拼团已成团", constants.EventGroupFailed: "拼团未成团", constants.EventShipmentDispatched: "商品已发货", constants.EventDeliveryConfirmed: "配送已签收", constants.EventOrderCompleted: "订单已完成", constants.EventRefundSucceeded: "退款已完成", constants.EventInvoiceIssued: "电子发票已开具", constants.EventTicketReplied: "工单有新回复",
}

func defaultTemplate(id string) repository.Template {
	if id == constants.EventProductPriceReached {
		return repository.Template{Title: notificationTitles[id], Body: "您关注的 {{product_name}} 已降至 ¥{{amount}}，前往商品页查看。", Enabled: true}
	}
	if id == constants.EventProductBackInStock {
		return repository.Template{Title: notificationTitles[id], Body: "您关注的 {{product_name}} 已有库存，前往商品页查看。", Enabled: true}
	}
	return repository.Template{Title: notificationTitles[id], Body: "业务编号 {{business_id}}", Enabled: true}
}
func (s *Service) TemplatesAdmin(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "notification.usecase.TemplatesAdmin")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	keyword := strings.ToLower(strings.TrimSpace(platform.Text(c.Data, "keyword")))
	if len([]rune(keyword)) > 120 {
		return nil, bad("template search is limited to 120 characters")
	}
	items := []platform.Result{}
	for id := range notificationTitles {
		v, version, e := s.repo.Template(ctx, id, defaultTemplate(id))
		if e != nil {
			return nil, e
		}
		if keyword != "" && !strings.Contains(strings.ToLower(id+" "+v.Title+" "+v.Body), keyword) {
			continue
		}
		items = append(items, platform.Result{"id": id, "event_type": id, "title": v.Title, "body": v.Body, "enabled": v.Enabled, "version": version})
	}
	return platform.Result{"items": items, "total": len(items)}, nil
}
func (s *Service) TemplateUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "notification.usecase.TemplateUpsert")
	defer span.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if _, ok := notificationTitles[c.ID]; !ok {
		return nil, bad("unsupported notification event")
	}
	var v repository.Template
	if e := platform.Decode(c.Data, &v); e != nil {
		return nil, e
	}
	if strings.TrimSpace(v.Title) == "" || strings.TrimSpace(v.Body) == "" || len([]rune(v.Title)) > 120 || len([]rune(v.Body)) > 2000 {
		return nil, bad("notification title and body required")
	}
	remainder := regexp.MustCompile(`\{\{(business_id|event_type|amount|product_name|sku_id)\}\}`).ReplaceAllString(v.Title+v.Body, "")
	if strings.Contains(remainder, "{{") || strings.Contains(remainder, "}}") {
		return nil, bad("template supports business_id, event_type, amount, product_name and sku_id only")
	}
	return s.repo.TemplateUpsert(ctx, c, v)
}
func renderNotification(text string, e platform.Event) string {
	amount := platform.Int(e.Data, "amount_cents")
	money := strconv.FormatInt(amount/100, 10) + "." + strings.TrimPrefix(strconv.FormatInt(amount%100+100, 10), "1")
	return strings.NewReplacer("{{business_id}}", e.AggregateID, "{{event_type}}", e.Type, "{{amount}}", money, "{{product_name}}", platform.Text(e.Data, "product_name"), "{{sku_id}}", platform.Text(e.Data, "sku_id")).Replace(text)
}
