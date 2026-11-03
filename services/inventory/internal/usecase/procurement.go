package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/inventory/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
	"strings"
)

func purchaseHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Service) ProcurementList(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.PurchaseList(ctx, c)
}
func (s *Service) ProcurementGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.PurchaseGet(ctx, c.ID)
}
func (s *Service) ProcurementCreate(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		SupplierID string                `json:"supplier_id"`
		Supplier   string                `json:"supplier"`
		Contact    string                `json:"supplier_contact"`
		Note       string                `json:"note"`
		Lines      []domain.PurchaseLine `json:"lines"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	leadDays := int64(0)
	if in.SupplierID != "" {
		row, e := s.Repo.Supplier(ctx, in.SupplierID)
		if e != nil {
			return nil, e
		}
		in.Supplier, in.Contact, leadDays = row.Name, row.Contact, row.LeadDays
	}
	in.Supplier = strings.TrimSpace(in.Supplier)
	if in.Supplier == "" || len(in.Supplier) > 120 || len(in.Contact) > 160 || len(in.Note) > 500 ||
		len(in.Lines) == 0 ||
		len(in.Lines) > 100 ||
		len(c.IdempotencyKey) < 8 ||
		len(c.IdempotencyKey) > 128 {
		return nil, status.Error(codes.InvalidArgument, "请填写供应商、有效采购明细和请求编号")
	}
	ids := []string{}
	seen := map[string]bool{}
	for i, line := range in.Lines {
		if line.SKUID == "" || len(line.SKUID) > 80 || seen[line.SKUID] || line.Quantity < 1 ||
			line.Quantity > 1000000 ||
			line.UnitCostCents < 0 ||
			line.UnitCostCents > 1000000000 {
			return nil, status.Error(codes.InvalidArgument, "采购规格需唯一，数量1至100万，成本为非负整数分")
		}
		seen[line.SKUID] = true
		ids = append(ids, line.SKUID)
		in.Lines[i].Received = 0
		in.Lines[i].Damaged = 0
	}
	result, e := platform.NewClients(
		s.RPC,
	).Catalog.BatchGet(
		ctx,
		platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"ids": ids}},
	)
	if e != nil {
		return nil, e
	}
	var products struct {
		Items []map[string]any `json:"items"`
	}
	if e = platform.Decode(result, &products); e != nil {
		return nil, e
	}
	if len(products.Items) != len(ids) {
		return nil, status.Error(codes.InvalidArgument, "存在未建档的采购规格，请先维护商品")
	}
	sort.Slice(in.Lines, func(i, j int) bool { return in.Lines[i].SKUID < in.Lines[j].SKUID })
	raw, _ := json.Marshal(in.Lines)
	id := "PO_" + purchaseHash([]string{c.UserID, c.IdempotencyKey})[:32]
	return s.Repo.CreatePurchase(
		ctx,
		&domain.PurchaseOrder{
			ID:              id,
			Supplier:        in.Supplier,
			SupplierID:      in.SupplierID,
			LeadDays:        leadDays,
			SupplierContact: in.Contact,
			Note:            in.Note,
			Status:          constants.PurchaseStatusDraft,
			ActorID:         c.UserID,
			Fingerprint:     purchaseHash(in),
			Lines:           string(raw),
		},
	)
}
func (s *Service) ProcurementApprove(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if _, ok := c.Data["expected_version"]; !ok {
		return nil, status.Error(codes.InvalidArgument, "请提供采购单版本")
	}
	return s.Repo.PurchaseTransition(ctx, c, constants.PurchaseStatusApproved)
}
func (s *Service) ProcurementCancel(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	if _, ok := c.Data["expected_version"]; !ok {
		return nil, status.Error(codes.InvalidArgument, "请提供采购单版本")
	}
	reason := strings.TrimSpace(platform.Text(c.Data, "reason"))
	if reason == "" || len(reason) > 240 {
		return nil, status.Error(codes.InvalidArgument, "请填写取消原因，最多240字节")
	}
	return s.Repo.PurchaseTransition(ctx, c, constants.PurchaseStatusCancelled)
}
func (s *Service) ProcurementReceive(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Lines []domain.PurchaseReceiveLine `json:"lines"`
		Note  string                       `json:"note"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if len(in.Lines) == 0 || len(in.Lines) > 100 || len(in.Note) > 500 || len(c.IdempotencyKey) < 8 ||
		len(c.IdempotencyKey) > 128 {
		return nil, status.Error(codes.InvalidArgument, "请填写到货明细及请求编号")
	}
	seen := map[string]bool{}
	for _, line := range in.Lines {
		if line.SKUID == "" || len(line.SKUID) > 80 || seen[line.SKUID] || line.Quantity < 1 ||
			line.Quantity > 1000000 ||
			line.Damaged < 0 ||
			line.Damaged > line.Quantity {
			return nil, status.Error(codes.InvalidArgument, "到货数量或损坏数量无效")
		}
		seen[line.SKUID] = true
	}
	sort.Slice(in.Lines, func(i, j int) bool { return in.Lines[i].SKUID < in.Lines[j].SKUID })
	id := "RCV_" + purchaseHash([]string{c.ID, c.IdempotencyKey})[:32]
	return s.Repo.ReceivePurchase(ctx, c, in.Lines, id, purchaseHash(in), in.Note)
}
