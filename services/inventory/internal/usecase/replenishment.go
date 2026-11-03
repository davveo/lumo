package usecase

import (
	"context"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
)

func supplyBad(text string) error { return status.Error(codes.InvalidArgument, text) }
func (s *Service) SupplierList(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.SupplierList(ctx, c)
}
func (s *Service) SupplierUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "inventory.usecase.SupplierUpsert")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var row domain.Supplier
	if e := platform.Decode(c.Data, &row); e != nil {
		return nil, e
	}
	row.Name = strings.TrimSpace(row.Name)
	row.Contact = strings.TrimSpace(row.Contact)
	if row.Name == "" || len(row.Name) > 120 || len(row.Contact) > 160 || row.LeadDays < 0 || row.LeadDays > 365 ||
		row.Version < 0 {
		return nil, supplyBad("供应商名称、联系方式或提前期无效")
	}
	row.ID = c.ID
	if row.ID == "" {
		row.ID = platform.ID("supplier")
	}
	if len(row.ID) > 128 {
		return nil, supplyBad("invalid supplier id")
	}
	row.UpdatedBy = c.UserID
	return s.Repo.SupplierUpsert(ctx, row, row.Version)
}
func (s *Service) ReplenishmentGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	ctx, sp := platform.Start(ctx, "inventory.usecase.ReplenishmentGet")
	defer sp.End()
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		IDs          []string `json:"ids"`
		SupplierID   string   `json:"supplier_id"`
		CoverageDays int64    `json:"coverage_days"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if in.CoverageDays == 0 {
		in.CoverageDays = 14
	}
	if len(in.IDs) < 1 || len(in.IDs) > 100 || in.SupplierID == "" || in.CoverageDays < 1 || in.CoverageDays > 365 {
		return nil, supplyBad("请选择供应商、1至100个规格和有效备货天数")
	}
	seen := map[string]bool{}
	for _, id := range in.IDs {
		if id == "" || len(id) > 80 || seen[id] {
			return nil, supplyBad("规格编号需唯一且有效")
		}
		seen[id] = true
	}
	supplier, e := s.Repo.Supplier(ctx, in.SupplierID)
	if e != nil {
		return nil, e
	}
	stocks, inbound, e := s.Repo.ReplenishmentInputs(ctx, in.IDs)
	if e != nil {
		return nil, e
	}
	clients := platform.NewClients(s.RPC)
	catalog, e := clients.Catalog.BatchGet(
		ctx,
		platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"ids": in.IDs}},
	)
	if e != nil {
		return nil, e
	}
	var products struct {
		Items []platform.Result `json:"items"`
	}
	if e = platform.Decode(catalog, &products); e != nil {
		return nil, e
	}
	if len(products.Items) != len(in.IDs) {
		return nil, supplyBad("存在未建档的规格")
	}
	sales, e := clients.Analytics.SkuSales(
		ctx,
		platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"ids": in.IDs, "days": 30}},
	)
	if e != nil {
		return nil, e
	}
	var volume struct {
		Items []struct {
			SKUID    string `json:"sku_id"`
			Quantity int64  `json:"quantity"`
		} `json:"items"`
		Ready bool `json:"forecast_ready"`
	}
	if e = platform.Decode(sales, &volume); e != nil {
		return nil, e
	}
	units := map[string]int64{}
	for _, row := range volume.Items {
		units[row.SKUID] = row.Quantity
	}
	items := []platform.Result{}
	for _, p := range products.Items {
		id := platform.Text(p, "id")
		stock := stocks[id]
		available := max(int64(0), stock.OnHand-stock.Reserved-stock.Allocated)
		target := stock.Threshold * 2
		basis := "按安全库存阈值补足，历史不足不推算日销量"
		if volume.Ready && units[id] > 0 {
			target = max(target, stock.Threshold+(units[id]*(supplier.LeadDays+in.CoverageDays)+29)/30)
			basis = "按已采集30天支付件数和提前期估算，未扣退货"
		}
		items = append(
			items,
			platform.Result{
				"sku_id":             id,
				"name":               p["name"],
				"available":          available,
				"inbound":            inbound[id],
				"paid_quantity":      units[id],
				"history_days":       30,
				"target":             target,
				"suggested_quantity": min(int64(1000000), max(int64(0), target-available-inbound[id])),
				"basis":              basis,
			},
		)
	}
	return platform.Result{
		"items":         items,
		"supplier":      supplier,
		"coverage_days": in.CoverageDays,
		"sales_scope":   sales["scope"],
	}, nil
}
