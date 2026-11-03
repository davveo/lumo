package usecase

import (
	"context"
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/points/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"time"
)

func (s *Service) Offers(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	return s.Repo.Offers(ctx, false)
}
func (s *Service) OffersAdmin(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.Offers(ctx, true)
}
func (s *Service) OfferUpsert(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	var offer domain.RedeemOffer
	if e := platform.Decode(c.Data, &offer); e != nil {
		return nil, e
	}
	if strings.TrimSpace(offer.Name) == "" || len([]rune(offer.Name)) > 120 || len([]rune(offer.Description)) > 500 ||
		offer.Cost < 1 ||
		offer.Cost > 100000000 ||
		offer.TotalQuota < 1 ||
		offer.TotalQuota > 1000000 ||
		offer.MaxPerUser < 1 ||
		offer.MaxPerUser > 100 ||
		!offer.EndsAt.After(offer.StartsAt) ||
		offer.ReferenceID == "" {
		return nil, status.Error(codes.InvalidArgument, "请填写有效的奖品、积分、名额和起止时间")
	}
	if offer.Kind == constants.RewardKindCoupon && offer.MaxPerUser != 1 {
		return nil, status.Error(codes.InvalidArgument, "优惠券兑换每人最多一次")
	}
	if e := s.validateReward(ctx, c, offer); e != nil {
		return nil, e
	}
	return s.Repo.OfferUpsert(ctx, c, offer)
}
func (s *Service) validateReward(ctx context.Context, c platform.Request, offer domain.RedeemOffer) error {
	clients := platform.NewClients(s.RPC)
	if offer.Kind == constants.RewardKindCoupon {
		data, e := clients.Coupon.TemplatesByIds(
			ctx,
			platform.Request{
				UserID: c.UserID,
				Role:   constants.RoleSystem,
				Data:   map[string]any{"ids": []string{offer.ReferenceID}},
			},
		)
		if e != nil {
			return e
		}
		var snapshot struct {
			Items []struct {
				ID        string    `json:"id"`
				Active    bool      `json:"active"`
				MinPoints int64     `json:"min_points"`
				MinGrowth int64     `json:"min_growth"`
				Remaining int64     `json:"remaining"`
				ExpiresAt time.Time `json:"expires_at"`
			}
		}
		if e = platform.Decode(data, &snapshot); e != nil {
			return e
		}
		if len(snapshot.Items) != 1 || !snapshot.Items[0].Active || snapshot.Items[0].MinPoints != 0 ||
			snapshot.Items[0].MinGrowth != 0 ||
			snapshot.Items[0].Remaining < 1 ||
			!snapshot.Items[0].ExpiresAt.After(time.Now()) {
			return status.Error(codes.FailedPrecondition, "请选择有效且无会员门槛的优惠券模板")
		}
		return nil
	}
	if offer.Kind == constants.RewardKindGift {
		item, e := clients.Catalog.Get(
			ctx,
			platform.Request{ID: offer.ReferenceID, UserID: c.UserID, Role: constants.RoleUser},
		)
		if e != nil {
			return e
		}
		if item["active"] == false {
			return status.Error(codes.FailedPrecondition, "实物奖品已下架")
		}
		return nil
	}
	return status.Error(codes.InvalidArgument, "不支持的奖品类型")
}
func (s *Service) Redeem(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	if len(c.IdempotencyKey) < 8 || len(c.IdempotencyKey) > 120 || c.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "兑换项目和唯一请求编号必填")
	}
	if old, ok, e := s.Repo.Replay(ctx, c); e != nil {
		return nil, e
	} else if ok {
		return platform.Map(old), nil
	}
	offer, e := s.Repo.Offer(ctx, c.ID)
	if e != nil {
		return nil, e
	}
	var address platform.Result
	if offer.Kind == constants.RewardKindGift {
		id := platform.Text(c.Data, "address_id")
		if id == "" {
			return nil, status.Error(codes.InvalidArgument, "实物兑换请选择收货地址")
		}
		address, e = platform.NewClients(
			s.RPC,
		).Address.Get(
			ctx,
			platform.Request{ID: id, UserID: c.UserID, Role: constants.RoleUser},
		)
		if e != nil {
			return nil, e
		}
		if platform.Text(address, "user_id") != c.UserID {
			return nil, status.Error(codes.PermissionDenied, "收货地址归属不匹配")
		}
	}
	// The locked offer version below prevents a reference or cost edit from
	// bypassing the confirmation made against this snapshot.
	if e = s.validateReward(ctx, c, offer); e != nil {
		return nil, e
	}
	return s.Repo.Redeem(ctx, c, address)
}
func (s *Service) Redemptions(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	return s.Repo.Redemptions(ctx, c, false)
}
func (s *Service) RedemptionsAdmin(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.Redemptions(ctx, c, true)
}
func (s *Service) RedemptionGet(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireUser(c); e != nil {
		return nil, e
	}
	owner := c.UserID
	if c.Role == constants.RoleSystem {
		owner = ""
	}
	row, e := s.Repo.Redemption(ctx, c.ID, owner)
	return platform.Map(row), e
}
func (s *Service) RedemptionRetry(ctx context.Context, c platform.Request) (platform.Result, error) {
	if e := platform.RequireAdmin(c); e != nil {
		return nil, e
	}
	return s.Repo.RetryRedemption(ctx, c)
}
func (s *Service) Tick(ctx context.Context) error { return s.Repo.QueueRedemptions(ctx) }
func definitiveRewardError(e error) bool {
	switch status.Code(e) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.NotFound, codes.ResourceExhausted:
		return true
	}
	return false
}
func (s *Service) processRedemption(ctx context.Context, id string) error {
	row, ok, e := s.Repo.AcquireRedemption(ctx, id)
	if e != nil || !ok {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := platform.Request{ID: row.ID, UserID: row.UserID, Role: constants.RoleSystem, IdempotencyKey: row.ID}
	clients := platform.NewClients(s.RPC)
	if row.Kind == constants.RewardKindCoupon {
		command.Data = map[string]any{"template_id": row.ReferenceID}
		out, callErr := clients.Coupon.IssueReward(ctx, command)
		if callErr != nil {
			next := constants.RedemptionStatePending
			if definitiveRewardError(callErr) {
				next = constants.RedemptionStateRefunded
			}
			return s.Repo.FinishRedemption(ctx, row, next, "", "", callErr.Error())
		}
		return s.Repo.FinishRedemption(
			ctx,
			row,
			constants.RedemptionStateIssued,
			platform.Text(out, "coupon_id"),
			"",
			"",
		)
	}
	lines := []map[string]any{{"sku_id": row.ReferenceID, "quantity": 1}}
	command.Data = map[string]any{"order_id": row.ID, "lines": lines}
	reserved, callErr := clients.Inventory.Reserve(ctx, command)
	if callErr != nil {
		next := constants.RedemptionStatePending
		if definitiveRewardError(callErr) {
			next = constants.RedemptionStateRefunded
		}
		return s.Repo.FinishRedemption(ctx, row, next, "", "", callErr.Error())
	}
	stockState := platform.Text(reserved, "state")
	if stockState != constants.InventoryReservationStateAllocated &&
		stockState != constants.InventoryReservationStateDispatched {
		if _, e = clients.Inventory.Confirm(ctx, command); e != nil {
			return s.Repo.FinishRedemption(ctx, row, constants.RedemptionStatePending, "", "", e.Error())
		}
	}

	var address map[string]any
	if e = json.Unmarshal([]byte(row.AddressJSON), &address); e != nil {
		return s.Repo.FinishRedemption(ctx, row, constants.RedemptionStateAttention, "", "", "收货地址快照异常，请人工跟进")
	}
	command.Data = map[string]any{"address": address, "lines": lines}
	out, e := clients.Fulfillment.CreateGift(ctx, command)
	if e != nil {
		return s.Repo.FinishRedemption(ctx, row, constants.RedemptionStatePending, "", "", e.Error())
	}
	return s.Repo.FinishRedemption(ctx, row, constants.RedemptionStateIssued, "", platform.Text(out, "id"), "")
}
