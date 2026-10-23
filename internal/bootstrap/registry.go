package bootstrap

import (
	"fmt"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/address"
	"github.com/davveo/lumo/services/aftersale"
	"github.com/davveo/lumo/services/analytics"
	"github.com/davveo/lumo/services/campaign"
	"github.com/davveo/lumo/services/cart"
	"github.com/davveo/lumo/services/catalog"
	"github.com/davveo/lumo/services/checkout"
	"github.com/davveo/lumo/services/content"
	"github.com/davveo/lumo/services/coupon"
	"github.com/davveo/lumo/services/engagement"
	"github.com/davveo/lumo/services/flash"
	"github.com/davveo/lumo/services/fulfillment"
	"github.com/davveo/lumo/services/group"
	"github.com/davveo/lumo/services/helpdesk"
	"github.com/davveo/lumo/services/iam"
	"github.com/davveo/lumo/services/interaction"
	"github.com/davveo/lumo/services/inventory"
	"github.com/davveo/lumo/services/invoice"
	"github.com/davveo/lumo/services/logistics"
	"github.com/davveo/lumo/services/media"
	"github.com/davveo/lumo/services/member"
	"github.com/davveo/lumo/services/notification"
	"github.com/davveo/lumo/services/order"
	"github.com/davveo/lumo/services/payment"
	"github.com/davveo/lumo/services/points"
	"github.com/davveo/lumo/services/pricing"
	"github.com/davveo/lumo/services/recommendation"
	"github.com/davveo/lumo/services/reconciliation"
	"github.com/davveo/lumo/services/refund"
	"github.com/davveo/lumo/services/search"
)

var Names = []string{"iam", "member", "address", "catalog", "media", "interaction", "engagement", "search", "recommendation", "content", "analytics", "cart", "pricing", "checkout", "order", "campaign", "coupon", "points", "group", "flash", "inventory", "fulfillment", "logistics", "aftersale", "payment", "refund", "invoice", "reconciliation", "notification", "helpdesk"}

func New(name string, d platform.Dependencies) (platform.Module, error) {
	switch name {
	case "iam":
		return iam.New(d), nil
	case "member":
		return member.New(d), nil
	case "address":
		return address.New(d), nil
	case "catalog":
		return catalog.New(d), nil
	case "media":
		return media.New(d), nil
	case "interaction":
		return interaction.New(d), nil
	case "engagement":
		return engagement.New(d), nil
	case "search":
		return search.New(d), nil
	case "recommendation":
		return recommendation.New(d), nil
	case "content":
		return content.New(d), nil
	case "analytics":
		return analytics.New(d), nil
	case "cart":
		return cart.New(d), nil
	case "pricing":
		return pricing.New(d), nil
	case "checkout":
		return checkout.New(d), nil
	case "order":
		return order.New(d), nil
	case "campaign":
		return campaign.New(d), nil
	case "coupon":
		return coupon.New(d), nil
	case "points":
		return points.New(d), nil
	case "group":
		return group.New(d), nil
	case "flash":
		return flash.New(d), nil
	case "inventory":
		return inventory.New(d), nil
	case "fulfillment":
		return fulfillment.New(d), nil
	case "logistics":
		return logistics.New(d), nil
	case "aftersale":
		return aftersale.New(d), nil
	case "payment":
		return payment.New(d), nil
	case "refund":
		return refund.New(d), nil
	case "invoice":
		return invoice.New(d), nil
	case "reconciliation":
		return reconciliation.New(d), nil
	case "notification":
		return notification.New(d), nil
	case "helpdesk":
		return helpdesk.New(d), nil
	default:
		return platform.Module{}, fmt.Errorf("unknown service %q", name)
	}
}
