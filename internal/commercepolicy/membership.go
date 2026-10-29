package commercepolicy

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
)

type Membership struct {
	Version          int64 `json:"version"`
	GoldGrowth       int64 `json:"gold_growth"`
	GrowthSpendCents int64 `json:"growth_spend_cents"`
}

func DefaultMembership() Membership { return Membership{GoldGrowth: 1000, GrowthSpendCents: 10000} }
func (p Membership) Growth(amount int64) int64 {
	n := amount / p.GrowthSpendCents
	if n < 1 {
		n = 1
	}
	return n
}
func ReadMembership(ctx context.Context, caller platform.Caller) (Membership, error) {
	value, e := platform.NewClients(caller).Member.PolicyGet(ctx, platform.Request{UserID: constants.RoleSystem, Role: constants.RoleSystem})
	if e != nil {
		return Membership{}, e
	}
	in := struct {
		Config Membership `json:"config"`
	}{Config: DefaultMembership()}
	if e = platform.Decode(value, &in); e != nil {
		return in.Config, e
	}
	in.Config.Version = platform.Int(value, "version")
	return in.Config, nil
}
