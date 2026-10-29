// Package commercepolicy defines immutable rule snapshots shared by pricing,
// checkout and event consumers. Each domain owns its editable rule storage.
package commercepolicy

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

type Points struct {
	Version       int64 `json:"version"`
	CheckinPoints int64 `json:"checkin_points"`
	DeductionBPS  int64 `json:"deduction_bps"`
	RewardPerYuan int64 `json:"reward_per_yuan"`
}

func DefaultPoints() Points {
	return Points{CheckinPoints: 20, DeductionBPS: 1000, RewardPerYuan: 1}
}
func (p Points) Limit(subtotal int64) int64 {
	return subtotal/10000*p.DeductionBPS + subtotal%10000*p.DeductionBPS/10000
}
func (p Points) Reward(amount int64) int64 { return amount / 100 * p.RewardPerYuan }
func ReadPoints(ctx context.Context, caller platform.Caller) (Points, error) {
	value, e := platform.NewClients(caller).Points.PolicyGet(ctx, platform.Request{UserID: constants.RoleSystem, Role: constants.RoleSystem})
	if e != nil {
		return Points{}, e
	}
	input := struct {
		Config Points `json:"config"`
	}{Config: DefaultPoints()}
	if e = platform.Decode(value, &input); e != nil {
		return input.Config, e
	}
	input.Config.Version = platform.Int(value, "version")
	return input.Config, nil
}
