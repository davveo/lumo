package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/commercepolicy"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
)

func (s *Service) PolicyGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	p, e := s.Repo.Policy(ctx)
	return platform.Result{"config": p, "version": p.Version}, e
}
func (s *Service) PolicyUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var in struct {
		Config commercepolicy.Aftersale `json:"config"`
	}
	if e := platform.Decode(c.Data, &in); e != nil {
		return nil, e
	}
	if !in.Config.Valid() {
		return nil, status.Error(codes.InvalidArgument, "invalid aftersale service policy")
	}
	return s.Repo.PolicyUpsert(ctx, c, in.Config)
}
func portion(n, part, total int64) int64 {
	if n <= 0 || part <= 0 || total <= 0 {
		return 0
	}
	if part >= total {
		return n
	}
	var value big.Int
	value.Mul(big.NewInt(n), big.NewInt(part))
	value.Quo(&value, big.NewInt(total))
	return value.Int64()
}
func (s *Service) Preview(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	typ := platform.Text(c.Data, "type")
	if typ != constants.AftersaleTypeRefund && typ != constants.AftersaleTypeReturn &&
		typ != constants.AftersaleTypeExchange {
		return nil, status.Error(codes.InvalidArgument, "valid aftersale type required")
	}
	id := platform.Text(c.Data, "order_id")
	o, e := platform.NewClients(s.RPC).Order.Get(ctx, platform.Request{ID: id, UserID: c.UserID, Role: c.Role})
	if e != nil {
		return nil, e
	}
	if platform.Text(o, "user_id") != c.UserID && !staff(c) {
		return nil, status.Error(codes.PermissionDenied, "order ownership mismatch")
	}
	prepared, e := prepareClaim(c, o, typ)
	if e != nil {
		return nil, e
	}
	lines, _ := json.Marshal(prepared.Requested)
	row := &domain.Record{
		UserID:      platform.Text(o, "user_id"),
		OrderID:     id,
		Type:        typ,
		LinesJSON:   string(lines),
		AmountCents: prepared.Amount,
	}
	if e = s.Repo.PreviewClaim(ctx, row, prepared.Purchased, prepared.Payable,stageLimits(o,prepared.Purchased,typ)); e != nil {
		return nil, e
	}
	var snapshot struct {
		Quote platform.Result `json:"quote"`
	}
	if e = platform.Decode(o, &snapshot); e != nil {
		return nil, e
	}
	points := platform.Int(snapshot.Quote, "points")
	already := platform.Int(o, "refunded_total_cents")
	next := min(prepared.Payable, already+row.AmountCents)
	returned := portion(points, next, prepared.Payable) - portion(points, already, prepared.Payable)
	policy, e := s.Repo.Policy(ctx)
	if e != nil {
		return nil, e
	}
	return platform.Result{
		"refund_cents":           row.AmountCents,
		"points_return_estimate": max(int64(0), returned),
		"coupon_id":              snapshot.Quote["coupon_id"],
		"coupon_rule":            "整单现金退款后返还原券，原有效期不延长；部分退款不返券。",
		"reward_rule":            "已发放的购物奖励积分将按退款比例回收。",
		"estimated":              true,
		"policy":                 policy,
		"notice":                 "预估不占用售后额度；最终以审核及退款处理结果为准。",
	}, nil
}
func (s *Service) EvidenceDownload(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	row, e := s.Repo.Get(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	if e = owner(c, row); e != nil {
		return nil, e
	}
	var evidence []platform.Result
	if json.Unmarshal([]byte(row.EvidenceJSON), &evidence) != nil {
		return nil, status.Error(codes.NotFound, "此售后单暂无图片凭证")
	}
	id := platform.Text(c.Data, "file_id")
	found := false
	for _, item := range evidence {
		if platform.Text(item, "file_id") == id {
			found = true
		}
	}
	if !found {
		return nil, status.Error(codes.PermissionDenied, "凭证未关联到此售后单")
	}
	return platform.NewClients(
		s.RPC,
	).Media.Download(
		ctx,
		platform.Request{ID: id, UserID: row.UserID, Role: constants.RoleSystem},
	)
}
