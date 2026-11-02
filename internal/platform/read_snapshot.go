package platform

import (
	"bytes"
	"context"
	"encoding/json"
	mallv1 "github.com/davveo/lumo/api/mall/v1"
	"github.com/davveo/lumo/internal/constants"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// This inventory is deliberately explicit: only operations with local database
// dependencies may run in a snapshot loop. No fallback to arbitrary actions.
var localReadActions = map[string]map[string]bool{}

func init() {
	for service, actions := range map[string][]string{
		constants.ServiceAddress:        {constants.ActionGet, constants.ActionList},
		constants.ServiceAftersale:      {constants.ActionGet, constants.ActionList},
		constants.ServiceAnalytics:      {constants.ActionList},
		constants.ServiceCampaign:       {constants.ActionList, constants.ActionEvaluate},
		constants.ServiceCart:           {constants.ActionList},
		constants.ServiceCatalog:        {constants.ActionGet, constants.ActionList, constants.ActionBatchGet},
		constants.ServiceContent:        {constants.ActionGet, constants.ActionList, constants.ActionPopupList},
		constants.ServiceCoupon:         {constants.ActionList},
		constants.ServiceEngagement:     {constants.ActionList},
		constants.ServiceFlash:          {constants.ActionGet, constants.ActionList},
		constants.ServiceFulfillment:    {constants.ActionGet, constants.ActionList},
		constants.ServiceGroup:          {constants.ActionGet, constants.ActionList},
		constants.ServiceHelpdesk:       {constants.ActionGet, constants.ActionList},
		constants.ServiceInventory:      {constants.ActionGet, constants.ActionBatchGet},
		constants.ServiceInvoice:        {constants.ActionGet, constants.ActionList, constants.ActionReversals},
		constants.ServiceLogistics:      {constants.ActionGet, constants.ActionList, constants.ActionQuote},
		constants.ServiceMember:         {constants.ActionGet},
		constants.ServiceNotification:   {constants.ActionList},
		constants.ServiceOrder:          {constants.ActionGet, constants.ActionList},
		constants.ServicePayment:        {constants.ActionGet, constants.ActionList},
		constants.ServicePoints:         {constants.ActionGet},
		constants.ServicePricing:        {constants.ActionGet},
		constants.ServiceReconciliation: {constants.ActionGet, constants.ActionList},
		constants.ServiceRefund:         {constants.ActionGet, constants.ActionList},
	} {
		localReadActions[service] = map[string]bool{}
		for _, action := range actions {
			localReadActions[service][action] = true
		}
	}
}
func LocalReadAllowed(service, action string) bool { return localReadActions[service][action] }

// ApplyReadSnapshot is also used by in-process test routers. It never calls RPC;
// module operations in the inventory above must remain local (guarded in tests).
func ApplyReadSnapshot(ctx context.Context, module Module, command Command) (Result, error) {
	if command.ID != "" && command.ID != module.Name {
		return nil, status.Error(codes.InvalidArgument, "snapshot domain mismatch")
	}
	var input struct {
		Requests []Command `json:"requests"`
	}
	if e := Decode(command.Data, &input); e != nil {
		return nil, e
	}
	if len(input.Requests) < 1 || len(input.Requests) > 64 {
		return nil, status.Error(codes.InvalidArgument, "snapshot requires 1..64 local reads")
	}
	// Authorize every child before executing any lazy local read/init operation.
	for _, request := range input.Requests {
		if !LocalReadAllowed(module.Name, request.Action) {
			return nil, status.Error(codes.PermissionDenied, "snapshot operation is not a local read")
		}
	}
	results := []Result{}
	for _, request := range input.Requests {
		var value Result
		var err error
		if ctx.Err() != nil {
			err = status.FromContextError(ctx.Err()).Err()
		} else if operation := module.Operations[request.Action]; operation != nil {
			value, err = operation(ctx, request.Request())
		} else if module.Handle != nil {
			value, err = module.Handle(ctx, request)
		} else {
			err = status.Error(codes.Unimplemented, "read unavailable")
		}
		item := Result{}
		if err != nil {
			item["error"] = Result{"code": int32(status.Code(err)), "message": status.Convert(err).Message()}
		} else {
			item["data"] = value
		}
		results = append(results, item)
	}
	return Result{"results": results}, nil
}
func (s *domainServer) ReadSnapshot(ctx context.Context, in *mallv1.Command) (*mallv1.Result, error) {
	var data map[string]any
	decoder := json.NewDecoder(bytes.NewReader(in.GetPayloadJson()))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid snapshot payload")
	}
	value, err := ApplyReadSnapshot(ctx, s.module, Command{ID: in.GetId(), Data: data})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &mallv1.Result{PayloadJson: raw}, nil
}
