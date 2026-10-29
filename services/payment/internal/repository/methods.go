package repository

import (
	"context"
	"github.com/davveo/lumo/internal/adminconfig"
	"github.com/davveo/lumo/internal/platform"
	"os"
)

type MethodsPolicy struct {
	Demo   bool `json:"demo_enabled"`
	Wechat bool `json:"wechat_enabled"`
	Alipay bool `json:"alipay_enabled"`
	Card   bool `json:"card_enabled"`
}

func (r *Repository) MethodsPolicy(ctx context.Context) (MethodsPolicy, int64, error) {
	return adminconfig.Read(ctx, r.db, "payment-methods", MethodsPolicy{Demo: os.Getenv("DEMO_MODE") == "true" || os.Getenv("DEMO_MODE") == "1"})
}
func (r *Repository) SaveMethodsPolicy(ctx context.Context, c platform.Request, p MethodsPolicy) (platform.Result, error) {
	return adminconfig.Save(ctx, r.db, "payment-methods", p, platform.Int(c.Data, "version"), c.UserID, platform.Text(c.Data, "reason"))
}
