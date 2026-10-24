package bootstrap

import (
	"github.com/davveo/lumo/internal/constants"

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

var Names = []string{
	constants.ServiceIAM,
	constants.ServiceMember,
	constants.ServiceAddress,
	constants.ServiceCatalog,
	constants.ServiceMedia,
	constants.ServiceInteraction,
	constants.ServiceEngagement,
	constants.ServiceSearch,
	constants.ServiceRecommendation,
	constants.ServiceContent,
	constants.ServiceAnalytics,
	constants.ServiceCart,
	constants.ServicePricing,
	constants.ServiceCheckout,
	constants.ServiceOrder,
	constants.ServiceCampaign,
	constants.ServiceCoupon,
	constants.ServicePoints,
	constants.ServiceGroup,
	constants.ServiceFlash,
	constants.ServiceInventory,
	constants.ServiceFulfillment,
	constants.ServiceLogistics,
	constants.ServiceAftersale,
	constants.ServicePayment,
	constants.ServiceRefund,
	constants.ServiceInvoice,
	constants.ServiceReconciliation,
	constants.ServiceNotification,
	constants.ServiceHelpdesk,
}

func New(name string, d platform.Dependencies) (platform.Module, error) {
	switch name {
	case constants.ServiceIAM:
		return iam.New(d), nil
	case constants.ServiceMember:
		return member.New(d), nil
	case constants.ServiceAddress:
		return address.New(d), nil
	case constants.ServiceCatalog:
		return catalog.New(d), nil
	case constants.ServiceMedia:
		return media.New(d), nil
	case constants.ServiceInteraction:
		return interaction.New(d), nil
	case constants.ServiceEngagement:
		return engagement.New(d), nil
	case constants.ServiceSearch:
		return search.New(d), nil
	case constants.ServiceRecommendation:
		return recommendation.New(d), nil
	case constants.ServiceContent:
		return content.New(d), nil
	case constants.ServiceAnalytics:
		return analytics.New(d), nil
	case constants.ServiceCart:
		return cart.New(d), nil
	case constants.ServicePricing:
		return pricing.New(d), nil
	case constants.ServiceCheckout:
		return checkout.New(d), nil
	case constants.ServiceOrder:
		return order.New(d), nil
	case constants.ServiceCampaign:
		return campaign.New(d), nil
	case constants.ServiceCoupon:
		return coupon.New(d), nil
	case constants.ServicePoints:
		return points.New(d), nil
	case constants.ServiceGroup:
		return group.New(d), nil
	case constants.ServiceFlash:
		return flash.New(d), nil
	case constants.ServiceInventory:
		return inventory.New(d), nil
	case constants.ServiceFulfillment:
		return fulfillment.New(d), nil
	case constants.ServiceLogistics:
		return logistics.New(d), nil
	case constants.ServiceAftersale:
		return aftersale.New(d), nil
	case constants.ServicePayment:
		return payment.New(d), nil
	case constants.ServiceRefund:
		return refund.New(d), nil
	case constants.ServiceInvoice:
		return invoice.New(d), nil
	case constants.ServiceReconciliation:
		return reconciliation.New(d), nil
	case constants.ServiceNotification:
		return notification.New(d), nil
	case constants.ServiceHelpdesk:
		return helpdesk.New(d), nil
	default:
		return platform.Module{}, fmt.Errorf("unknown service %q", name)
	}
}
