package httpgateway

import (
	"context"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

// This read model is exclusively for the operations dashboard. Domain lists,
// configuration and business writes remain separate, named service operations.
func (g *gateway) managementDashboard(ctx context.Context, c platform.Command) (platform.Result, error) {
	if err := platform.RequireAdmin(c); err != nil && !platform.StaffAllows(c.Role, "management", c.Action) {
		return nil, err
	}
	if c.Action == constants.ActionCustomerProfile {
		return g.customerProfile(ctx, c)
	}
	if c.Action == constants.ActionGlobalSearch {
		return g.managementSearch(ctx, c)
	}
	if c.Action == constants.ActionTasks || c.Action == constants.ActionRetry {
		return g.managementTasks(ctx, c)
	}
	if c.Action != constants.ActionDashboard {
		return nil, status.Error(codes.Unimplemented, "unsupported management operation")
	}
	days := platform.Int(c.Data, "days")
	if days == 0 {
		days = 7
	}
	if days != 7 && days != 30 && days != 90 {
		return nil, status.Error(codes.InvalidArgument, "days must be 7, 30 or 90")
	}
	clients := platform.NewClients(g.rpc)

	result := platform.Result{"days": days, "timezone": "Asia/Shanghai"}
	failures := map[string]string{}
	var lock sync.Mutex
	var group sync.WaitGroup
	read := func(name string, read platform.Operation) {
		defer group.Done()
		value, err := read(
			ctx,
			platform.Request{UserID: c.UserID, Role: c.Role, Data: map[string]any{"days": days}},
		)
		lock.Lock()
		defer lock.Unlock()
		if err != nil {
			failures[name] = status.Code(err).String()
		} else {
			result[name] = value
		}
	}
	group.Add(8)
	go read("order", clients.Order.AdminSummary)
	go read("payment", clients.Payment.AdminSummary)
	go read("fulfillment", clients.Fulfillment.AdminSummary)
	go read("aftersale", clients.Aftersale.AdminSummary)
	go read("refund", clients.Refund.AdminSummary)
	go read("invoice", clients.Invoice.AdminSummary)
	go read("users", clients.IAM.AdminSummary)
	go read("insights", clients.Analytics.Dashboard)

	group.Wait()
	result["errors"] = failures
	result["degraded"] = len(failures) > 0
	return result, nil
}

func init() {
	adminActions["management/global_search"] = true
	reads[constants.ActionGlobalSearch] = true
	userActions["logistics/tracking"] = true
	reads["tracking"] = true
	for _, a := range []string{"tracking_update", "exceptions", "exception_resolve"} {
		adminActions["logistics/"+a] = true
	}
	reads["exceptions"] = true

	for _, a := range []string{
		"procurement_list",
		"procurement_get",
		"procurement_create",
		"procurement_approve",
		"procurement_receive",
		"procurement_cancel",
	} {
		adminActions["inventory/"+a] = true
	}
	reads["procurement_get"] = true
	reads["procurement_list"] = true

	for _, a := range []string{"import_preview", "import_commit", "import_get"} {
		adminActions["catalog/"+a] = true
	}
	reads["import_get"] = true

	for _, a := range []string{"staff_list", "staff_upsert"} {
		adminActions["iam/"+a] = true
	}
	reads["staff_list"] = true

	adminActions["analytics/dashboard"] = true
	adminActions["analytics/sync_history"] = true

	for _, a := range []string{"security_get", "password_change", "security_send", "security_verify", "session_revoke"} {
		userActions["iam/"+a] = true
	}
	for _, a := range []string{"password_reset_request", "password_reset"} {
		publicActions["iam/"+a] = true
	}
	reads["security_get"] = true

	for _, service := range []string{constants.ServiceIAM, constants.ServicePayment} {
		for _, action := range []string{constants.ActionProviderGet, constants.ActionProviderUpsert} {
			adminActions[service+"/"+action] = true
		}
	}
	reads[constants.ActionProviderGet] = true
	publicActions[constants.ServiceEngagement+"/"+constants.ActionPolicyGet] = true
	for _, a := range []string{constants.ActionSubscribe, constants.ActionSubscriptions, constants.ActionUnsubscribe} {
		userActions[constants.ServiceEngagement+"/"+a] = true
	}
	for _, a := range []string{constants.ActionPolicyUpsert, constants.ActionAdminList, constants.ActionAdminCancel} {
		adminActions[constants.ServiceEngagement+"/"+a] = true
	}
	reads[constants.ActionSubscriptions] = true
	for _, a := range []string{
		constants.ActionLoginMethods,
		constants.ActionOtpSend,
		constants.ActionOtpLogin,
		constants.ActionOAuthStart,
		constants.ActionOAuthCallback,
	} {
		publicActions[constants.ServiceIAM+"/"+a] = true
	}
	adminActions[constants.ServiceIAM+"/"+constants.ActionPolicyGet] = true
	adminActions[constants.ServiceIAM+"/"+constants.ActionPolicyUpsert] = true
	publicActions[constants.ServicePayment+"/"+constants.ActionMethods] = true
	adminActions[constants.ServicePayment+"/"+constants.ActionPolicyGet] = true
	adminActions[constants.ServicePayment+"/"+constants.ActionPolicyUpsert] = true
	userActions[constants.ServiceCoupon+"/"+constants.ActionCalculate] = true
	additions := actionSet(map[string][]string{
		constants.ServiceAnalytics: {constants.ActionDrilldown,
			constants.ActionAdminList,
		}, constants.ServiceHelpdesk: {
			constants.ActionAdminList,
			constants.ActionWorkQueue,
			constants.ActionWorkDetail,
			constants.ActionAssignWork,
			constants.ActionSaveView,
			constants.ActionViews,
			constants.ActionPolicyGet,
			constants.ActionPolicyUpsert,
		},
		constants.ServiceFlash: {
			constants.ActionAdminList,
			constants.ActionUpsert,
			constants.ActionRequestsAdmin,
		},
		constants.ServiceGroup: {
			constants.ActionAdminList,
			constants.ActionRulesAdmin,
			constants.ActionRuleUpsert,
		},
		constants.ServiceCampaign:       {constants.ActionRuleUpsert},
		constants.ServiceCatalog:        {constants.ActionAdminList},
		constants.ServiceInventory:      {constants.ActionAdminList, constants.ActionThreshold, constants.ActionLedger},
		constants.ServiceReconciliation: {constants.ActionAdminList, constants.ActionGet, constants.ActionNote},
		constants.ServicePoints: {
			constants.ActionPolicyUpsert,
			constants.ActionAdminGet,
			constants.ActionAdjust,
			constants.ActionLedger,
		},
		constants.ServiceMember:         {constants.ActionPolicyUpsert},
		constants.ServiceLogistics:      {constants.ActionPolicyGet, constants.ActionPolicyUpsert},
		constants.ServiceNotification:   {constants.ActionTemplatesAdmin, constants.ActionTemplateUpsert},
		constants.ServiceContent:        {constants.ActionAdminList, constants.ActionAdminUpsert},
		constants.ServiceRecommendation: {constants.ActionPolicyGet, constants.ActionPolicyUpsert},
		constants.ServiceSearch:         {constants.ActionPolicyGet, constants.ActionPolicyUpsert},
		constants.ServiceIAM:            {constants.ActionAuditList, constants.ActionAdminGet},
		constants.ServiceManagement:     {constants.ActionTasks, constants.ActionRetry},
	})
	for key := range additions {
		adminActions[key] = true
	}
	publicActions[constants.ServiceFlash+"/"+constants.ActionSessionGet] = true
	publicActions[constants.ServiceGroup+"/"+constants.ActionRules] = true
	userActions[constants.ServicePoints+"/"+constants.ActionPolicyGet] = true
	userActions[constants.ServiceMember+"/"+constants.ActionPolicyGet] = true
	for _, action := range []string{
		constants.ActionPolicyGet,
		constants.ActionMethods,
		constants.ActionLoginMethods,
		constants.ActionRules,
		constants.ActionRulesAdmin,
		constants.ActionSessionGet,
		constants.ActionRequestsAdmin,
		constants.ActionAdminGet,
		constants.ActionLedger,
		constants.ActionAuditList,
		constants.ActionTasks,
	} {
		reads[action] = true
	}
}
func (g *gateway) managementTasks(ctx context.Context, c platform.Command) (platform.Result, error) {
	service := platform.Text(c.Data, "service")
	allowed := false
	for _, name := range platform.ServiceNames() {
		if name == service {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, status.Error(codes.InvalidArgument, "select a known service")
	}
	request := c.Request()
	if c.Action == constants.ActionRetry {
		return platform.RetryServiceTask(ctx, g.rpc, service, request)
	}
	value, e := platform.ReadServiceTasks(ctx, g.rpc, service, request)
	if e == nil {
		value["services"] = platform.ServiceNames()
	}
	return value, e
}
