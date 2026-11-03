package httpgateway

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Search fans out once per permitted domain. No source can borrow another
// domain's privilege, and only the navigation fields enter the response.
func (g *gateway) managementSearch(ctx context.Context, c platform.Command) (platform.Result, error) {
	query := strings.TrimSpace(platform.Text(c.Data, "query"))
	if utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 100 {
		return nil, status.Error(codes.InvalidArgument, "请输入2至100字的业务编号、商品或会员名称")
	}
	clients := platform.NewClients(g.rpc)
	groups := make([]platform.Result, 6)
	var wg sync.WaitGroup
	read := func(index int, service, action, label, tab string, op platform.Operation) {
		defer wg.Done()
		if !platform.StaffAllows(c.Role, service, action) {
			return
		}
		req := platform.Request{
			UserID: c.UserID,
			Role:   c.Role,
			Data:   map[string]any{"keyword": query, "page": 1, "page_size": 5},
		}
		if service == "iam" {
			req.Data["status"] = "user"
		}
		if service == "logistics" {
			req.ID = query
		}
		result, err := op(ctx, req)
		group := platform.Result{"label": label, "items": []platform.Result{}, "service": service}
		if err != nil {
			if status.Code(err) != codes.NotFound {
				group["error"] = "暂时无法读取，请稍后重试"
			}
			groups[index] = group
			return
		}
		var rows struct {
			Items     []platform.Result `json:"items"`
			Shipments []platform.Result `json:"shipments"`
		}
		if err = platform.Decode(result, &rows); err != nil {
			group["error"] = "结果读取失败"
			groups[index] = group
			return
		}
		if service == "logistics" {
			rows.Items = rows.Shipments
		}
		items := []platform.Result{}
		for _, row := range rows.Items {
			if len(items) == 5 {
				break
			}
			id := platform.Text(row, "id")
			name := platform.Text(row, "name")
			if service == "iam" {
				name = platform.Text(row, "username")
			}
			if service == "logistics" {
				name = platform.Text(row, "tracking_id")
				if !platform.StaffAllows(c.Role, "logistics", "exceptions") {
					tab = "orders"
					id = platform.Text(row, "order_id")
				}
			}
			if name == "" {
				name = id
			}
			items = append(
				items,
				platform.Result{
					"id":        id,
					"label":     name,
					"tab":       tab,
					"status":    row["status"],
					"reference": platform.Text(row, "order_id"),
				},
			)
		}
		group["items"] = items
		groups[index] = group
	}
	wg.Add(6)
	go read(0, "order", "admin_list", "订单", "orders", clients.Order.AdminList)
	go read(1, "aftersale", "admin_list", "售后", "aftersales", clients.Aftersale.AdminList)
	go read(2, "refund", "admin_list", "退款", "refunds", clients.Refund.AdminList)
	go read(3, "catalog", "admin_list", "商品 / SKU", "products", clients.Catalog.AdminList)
	go read(4, "iam", "admin_list", "会员", "users", clients.IAM.AdminList)
	go read(5, "logistics", "tracking", "运单", "shipments", clients.Logistics.Tracking)
	wg.Wait()
	visible := []platform.Result{}
	for _, group := range groups {
		if group != nil {
			visible = append(visible, group)
		}
	}
	return platform.Result{"groups": visible, "query": query}, nil
}
