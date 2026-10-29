package usecase

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/couponrules"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
	"time"
)

func (s *Service) chooseCoupon(ctx context.Context, c platform.Request, mode, id string, subtotal, remaining int64, lines []couponrules.Line) (string, int64, []platform.Result, error) {
	ctx, span := platform.Start(ctx, "pricing.usecase.ChooseCoupon")
	defer span.End()
	options := []platform.Result{}
	if mode != "" && mode != "auto" && mode != "manual" && mode != "none" {
		return "", 0, nil, status.Error(codes.InvalidArgument, "invalid coupon selection mode")
	}
	if mode == "none" || mode != "auto" && id == "" {
		return "", 0, options, nil
	}
	value, e := platform.NewClients(s.rpc).Coupon.List(ctx, platform.Request{UserID: c.UserID, Role: c.Role})
	if e != nil {
		return "", 0, nil, e
	}
	var data struct{ Items []map[string]any }
	if e = platform.Decode(value, &data); e != nil {
		return "", 0, nil, e
	}
	selected := ""
	best := int64(0)
	found := false
	for _, row := range data.Items {
		cid := platform.Text(row, "id")
		why := ""
		discount := int64(0)
		st := platform.Text(row, "status")
		if st != constants.CouponStatusAvailable && st != constants.CouponStatusAvailableLegacy {
			why = "优惠券不可用"
		}
		expires := platform.Text(row, "expires_at")
		if expires != "" {
			t, err := time.Parse(time.RFC3339Nano, expires)
			if err != nil || !time.Now().Before(t) {
				why = "优惠券已过期"
			}
		}
		min := platform.Int(row, "min_cents")
		if min == 0 {
			min = platform.Int(row, "min_spend_cents")
		}
		amount := platform.Int(row, "amount_cents")
		if amount == 0 {
			amount = platform.Int(row, "discount_cents")
		}
		var rule couponrules.Rule
		if err := platform.Decode(row, &rule); err != nil {
			why = "优惠规则无效"
		}
		if why == "" {
			var err error
			discount, err = couponrules.Discount(rule, amount, min, subtotal, lines)
			if err != nil {
				why = err.Error()
			}
		}
		if why == "" && discount <= 0 {
			why = "当前商品无可抵扣金额"
		}
		if why == "" && discount >= remaining {
			why = "优惠后需保留有效商品实付金额"
		}
		options = append(options, platform.Result{"id": cid, "name": platform.Text(row, "template_name"), "eligible": why == "", "reason": why, "discount_cents": discount, "expires_at": expires})
		if cid == id {
			found = true
			if mode != "auto" {
				if why != "" {
					return "", 0, options, status.Error(codes.FailedPrecondition, why)
				}
				selected = cid
				best = discount
			}
		}
	}
	sort.SliceStable(options, func(i, j int) bool {
		a, b := options[i], options[j]
		if a["eligible"] != b["eligible"] {
			return a["eligible"] == true
		}
		if platform.Int(a, "discount_cents") != platform.Int(b, "discount_cents") {
			return platform.Int(a, "discount_cents") > platform.Int(b, "discount_cents")
		}
		ea, eb := platform.Text(a, "expires_at"), platform.Text(b, "expires_at")
		if ea != eb {
			if ea == "" {
				return false
			}
			if eb == "" {
				return true
			}
			return ea < eb
		}
		return platform.Text(a, "id") < platform.Text(b, "id")
	})
	if mode == "auto" {
		for _, o := range options {
			if o["eligible"] == true {
				selected = platform.Text(o, "id")
				best = platform.Int(o, "discount_cents")
				break
			}
		}
	} else if !found {
		return "", 0, options, status.Error(codes.NotFound, fmt.Sprintf("coupon %s not found", id))
	}
	for _, o := range options {
		o["selected"] = platform.Text(o, "id") == selected
	}
	return selected, best, options, nil
}
