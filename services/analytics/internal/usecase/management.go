package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/adminquery"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

var behaviorLabels = map[string]string{constants.AnalyticsBehaviorView: "浏览商品", constants.AnalyticsBehaviorClick: "点击商品", constants.AnalyticsBehaviorImpression: "商品曝光", constants.AnalyticsBehaviorSearch: "搜索商品", constants.EventOrderCompleted: "完成订单", constants.EventRefundSucceeded: "退款成功"}

func (s *Service) AdminList(ctx context.Context, c platform.Request) (spanResult0 platform.Result, spanErr0 error) {
	ctx, sp := platform.Start(ctx, "analytics.usecase.AdminList")
	defer sp.End()
	defer func() { platform.TraceError(ctx, spanErr0) }()

	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if v := platform.Text(c.Data, "event_type"); v != "" && behaviorLabels[v] == "" {
		return nil, bad("unsupported behavior filter")
	}
	if v := platform.Text(c.Data, "status"); v != "" && v != constants.AnalyticsKindBehavior && v != constants.AnalyticsKindPurchaseEvent {
		return nil, bad("unsupported record category")
	}
	rows, total, f, e := s.repo.AdminList(ctx, c)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, r := range rows {
		sku := platform.Text(payload(r), "sku_id")
		if sku != "" && !seen[sku] {
			ids = append(ids, sku)
			seen[sku] = true
		}
	}
	products := map[string]string{}
	catalogError := false
	if len(ids) > 0 && s.rpc != nil {
		v, e := platform.NewClients(s.rpc).Catalog.BatchGet(ctx, platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"ids": ids}})
		if e != nil {
			catalogError = true
		} else {
			var data struct {
				Items []map[string]any `json:"items"`
			}
			if e = platform.Decode(v, &data); e != nil {
				return nil, e
			}
			for _, p := range data.Items {
				products[platform.Text(p, "id")] = platform.Text(p, "name")
			}
		}
	}
	items := []platform.Result{}
	for _, r := range rows {
		v := view(r)
		kind := platform.Text(v, "type")
		label := behaviorLabels[kind]
		if label == "" {
			label = "其他行为"
		}
		v["action_label"] = label
		if kind == constants.EventRefundSucceeded && platform.Text(v, "refund_id") == "" {
			v["refund_id"] = platform.Text(v, "order_id")
			v["order_id"] = ""
		}
		sku, query, order := platform.Text(v, "sku_id"), platform.Text(v, "query"), platform.Text(v, "order_id")
		name := products[sku]
		if name == "" {
			name = platform.Text(v, "product_name")
		}
		v["product_name"] = name
		summary := label
		if query != "" {
			summary += "：" + query
		} else if sku != "" {
			name := platform.Text(v, "product_name")
			if name == "" {
				name = "商品 " + sku
			}
			summary += "：" + name
		} else if refund := platform.Text(v, "refund_id"); refund != "" {
			summary += "：退款单 " + refund
		} else if order != "" {
			summary += "：" + order
		} else {
			summary += "（历史记录未提供对象）"
		}
		v["summary"] = summary
		v["record_category"] = map[string]string{constants.AnalyticsKindBehavior: "访问互动", constants.AnalyticsKindPurchaseEvent: "交易结果"}[r.Kind]
		items = append(items, v)
	}
	result := adminquery.Result(items, total, f)
	result["catalog_unavailable"] = catalogError
	return result, nil
}
