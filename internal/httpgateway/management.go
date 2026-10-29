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
	if err := platform.RequireAdmin(c); err != nil {
		return nil, err
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
	if days != 7 && days != 30 {
		return nil, status.Error(codes.InvalidArgument, "days must be 7 or 30")
	}
	clients := platform.NewClients(g.rpc)
	calls := map[string]platform.Operation{
		"order":       clients.Order.AdminSummary,
		"payment":     clients.Payment.AdminSummary,
		"fulfillment": clients.Fulfillment.AdminSummary,
		"aftersale":   clients.Aftersale.AdminSummary,
		"refund":      clients.Refund.AdminSummary,
		"invoice":     clients.Invoice.AdminSummary,
		"users":       clients.IAM.AdminSummary,
	}
	result := platform.Result{"days": days, "timezone": "Asia/Shanghai"}
	failures := map[string]string{}
	var lock sync.Mutex
	var group sync.WaitGroup
	for name, read := range calls {
		group.Add(1)
		go func(name string, read platform.Operation) {
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
		}(name, read)
	}
	group.Wait()
	result["errors"] = failures
	result["degraded"] = len(failures) > 0
	return result, nil
}

func init() {
	publicActions[constants.ServiceEngagement+"/"+constants.ActionPolicyGet] = true
	for _, a := range []string{constants.ActionSubscribe, constants.ActionSubscriptions, constants.ActionUnsubscribe} {
		userActions[constants.ServiceEngagement+"/"+a] = true
	}
	for _, a := range []string{constants.ActionPolicyUpsert, constants.ActionAdminList, constants.ActionAdminCancel} {
		adminActions[constants.ServiceEngagement+"/"+a] = true
	}
	reads[constants.ActionSubscriptions] = true
	for _, a := range []string{constants.ActionLoginMethods, constants.ActionOtpSend, constants.ActionOtpLogin, constants.ActionOAuthStart, constants.ActionOAuthCallback} {
		publicActions[constants.ServiceIAM+"/"+a] = true
	}
	adminActions[constants.ServiceIAM+"/"+constants.ActionPolicyGet] = true
	adminActions[constants.ServiceIAM+"/"+constants.ActionPolicyUpsert] = true
	publicActions[constants.ServicePayment+"/"+constants.ActionMethods] = true
	adminActions[constants.ServicePayment+"/"+constants.ActionPolicyGet] = true
	adminActions[constants.ServicePayment+"/"+constants.ActionPolicyUpsert] = true
	userActions[constants.ServiceCoupon+"/"+constants.ActionCalculate] = true
	additions := actionSet(map[string][]string{
		constants.ServiceFlash:          {constants.ActionAdminList, constants.ActionUpsert, constants.ActionRequestsAdmin},
		constants.ServiceGroup:          {constants.ActionAdminList, constants.ActionRulesAdmin, constants.ActionRuleUpsert},
		constants.ServiceCampaign:       {constants.ActionRuleUpsert},
		constants.ServiceCatalog:        {constants.ActionAdminList},
		constants.ServiceInventory:      {constants.ActionAdminList, constants.ActionThreshold, constants.ActionLedger},
		constants.ServiceReconciliation: {constants.ActionAdminList, constants.ActionGet, constants.ActionNote},
		constants.ServicePoints:         {constants.ActionPolicyUpsert, constants.ActionAdminGet, constants.ActionAdjust, constants.ActionLedger},
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
	for _, action := range []string{constants.ActionPolicyGet, constants.ActionMethods, constants.ActionLoginMethods, constants.ActionRules, constants.ActionRulesAdmin, constants.ActionSessionGet, constants.ActionRequestsAdmin, constants.ActionAdminGet, constants.ActionLedger, constants.ActionAuditList, constants.ActionTasks} {
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
