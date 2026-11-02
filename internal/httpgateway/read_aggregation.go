package httpgateway

import (
	"github.com/davveo/lumo/internal/constants"

	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/davveo/lumo/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// These commands only read state. Do not infer safety from a verb such as
// "quote": pricing/checkout quotes persist records and must remain independent.
var aggregateReadActions = map[string]bool{
	constants.ActionGet:               true,
	constants.ActionList:              true,
	constants.ActionQuery:             true,
	constants.ActionEvaluate:          true,
	constants.ActionTemplates:         true,
	constants.ActionTemplatesSnapshot: true,
	constants.ActionFaq:               true,
	constants.ActionReversals:         true,
	constants.ActionBatchGet:          true,
	constants.ActionPopupList:         true,
}

type readCommand struct {
	ID   string         `json:"id,omitempty"`
	Data map[string]any `json:"data"`
}
type readRequest struct {
	Key     string      `json:"key"`
	Service string      `json:"service"`
	Action  string      `json:"action"`
	Command readCommand `json:"command"`
	auth    bool
}

func (g *gateway) storefront(ctx context.Context, c platform.Command) (platform.Result, error) {
	if c.Action == constants.ActionRecent {
		return g.storefrontRecent(ctx, c)
	}
	if c.Action == constants.ActionPopup || c.Action == constants.ActionPopupEvent ||
		c.Action == constants.ActionPopupClaim {
		return g.storefrontPopup(ctx, c)
	}
	if c.Action == constants.ActionBootstrap || c.Action == constants.ActionShell ||
		c.Action == constants.ActionProducts ||
		c.Action == constants.ActionActivities ||
		c.Action == constants.ActionAccountSummary {
		return g.storefrontSection(ctx, c)
	}
	var input struct {
		Requests []readRequest `json:"requests"`
	}
	b, err := json.Marshal(c.Data)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid read batch")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil || len(input.Requests) < 1 || len(input.Requests) > 64 {
		return nil, status.Error(codes.InvalidArgument, "read batch requires 1..64 strict read commands")
	}
	keys := map[string]bool{}
	for i := range input.Requests {
		item := &input.Requests[i]
		if item.Key == "" || len(item.Key) > 100 || keys[item.Key] {
			return nil, status.Error(codes.InvalidArgument, "unique read keys required")
		}
		keys[item.Key] = true
		item.auth = c.UserID != ""
		if item.Command.Data == nil {
			item.Command.Data = map[string]any{}
		}
		if (item.Service == constants.ServiceCatalog || item.Service == constants.ServiceInventory) &&
			item.Action == constants.ActionGet &&
			(len(item.Command.ID) > 150 || strings.TrimSpace(item.Command.ID) != item.Command.ID) {
			return nil, status.Error(codes.InvalidArgument, "invalid SKU ID")
		}
		if err := authorizeRead(*item, c); err != nil {
			return nil, err
		}
	}
	return platform.Result{"results": g.runReads(ctx, input.Requests, c)}, nil
}

func authorizeRead(item readRequest, principal platform.Command) error {
	key := item.Service + "/" + item.Action
	if item.Service == constants.ServiceStorefront || item.Service == constants.ServiceIAM ||
		(!aggregateReadActions[item.Action] && key != constants.ServiceLogistics+"/"+constants.ActionQuote) ||
		(!publicActions[key] && !userActions[key] && !adminActions[key]) {
		return status.Error(codes.PermissionDenied, "command is not an exposed read action")
	}
	if !publicActions[key] && principal.UserID == "" {
		return status.Error(codes.Unauthenticated, "Bearer token required for private reads")
	}
	if adminActions[key] && !platform.StaffAllows(principal.Role, item.Service, item.Action) {
		return status.Error(codes.PermissionDenied, "administrator required")
	}
	return nil
}

func canonicalRead(item readRequest) string {
	if item.Service == constants.ServiceCoupon && item.Action == constants.ActionTemplates &&
		len(item.Command.Data) == 0 {
		item.Command.Data = map[string]any{"page": 1, "page_size": 100}
	}
	b, _ := json.Marshal([]any{item.Service, item.Action, item.Command, item.auth})
	return string(b)
}
func readResult(item readRequest, data platform.Result, err error) platform.Result {
	result := platform.Result{
		"key":     item.Key,
		"service": item.Service,
		"action":  item.Action,
		"command": item.Command,
		"auth":    item.auth,
	}
	if err == nil {
		result["data"] = data
		return result
	}
	s, ok := status.FromError(err)
	if !ok {
		s = status.New(codes.Internal, "internal service error")
	}
	result["error"] = platform.Result{
		"code":    s.Code().String(),
		"message": s.Message(),
		"status":  aggregateHTTPStatus(s.Code()),
	}
	return result
}
func aggregateHTTPStatus(code codes.Code) int {
	switch code {
	case codes.InvalidArgument:
		return 400
	case codes.Unauthenticated:
		return 401
	case codes.PermissionDenied:
		return 403
	case codes.NotFound:
		return 404
	case codes.AlreadyExists, codes.Aborted, codes.FailedPrecondition:
		return 409
	case codes.ResourceExhausted:
		return 429
	case codes.Canceled:
		return 499
	case codes.Unimplemented:
		return 501
	case codes.Unavailable:
		return 503
	case codes.DeadlineExceeded:
		return 504
	default:
		return 500
	}
}

// A request contains a bounded number of jobs, each command is deduplicated,
// and all child RPCs retain the caller's context and trace. SQL bulk reads avoid
// merely moving the browser's per-SKU N+1 loop into the gateway.
func (g *gateway) runReads(ctx context.Context, requests []readRequest, principal platform.Command) []platform.Result {
	results := make([]platform.Result, len(requests))
	duplicates := map[string][]int{}
	bulk := map[string][]int{}
	for i, item := range requests {
		if (item.Service == constants.ServiceCatalog || item.Service == constants.ServiceInventory) &&
			item.Action == constants.ActionGet &&
			item.Command.ID != "" &&
			len(item.Command.Data) == 0 {
			bulk[fmt.Sprintf("%s:%t", item.Service, item.auth)] = append(
				bulk[fmt.Sprintf("%s:%t", item.Service, item.auth)],
				i,
			)
		} else {
			duplicates[canonicalRead(item)] = append(duplicates[canonicalRead(item)], i)
		}
	}
	type snapshotJob struct {
		command   platform.Command
		positions []int
		byID      map[string][]int
		ids       []string
	}
	jobs := map[string][]snapshotJob{}
	for _, positions := range duplicates {
		item := requests[positions[0]]
		command := platform.Command{Action: item.Action, ID: item.Command.ID, Data: item.Command.Data}
		if item.auth {
			command.UserID, command.Role = principal.UserID, principal.Role
		}
		jobs[item.Service] = append(jobs[item.Service], snapshotJob{command: command, positions: positions})
	}
	for _, positions := range bulk {
		byID, ids := map[string][]int{}, []string{}
		for _, position := range positions {
			id := requests[position].Command.ID
			if _, exists := byID[id]; !exists {
				ids = append(ids, id)
			}
			byID[id] = append(byID[id], position)
		}
		item := requests[positions[0]]
		for first := 0; first < len(ids); first += 500 {
			chunk := append([]string(nil), ids[first:min(first+500, len(ids))]...)
			command := platform.Command{Action: constants.ActionBatchGet, Data: map[string]any{"ids": chunk}}
			if item.auth {
				command.UserID, command.Role = principal.UserID, principal.Role
			}
			jobs[item.Service] = append(jobs[item.Service], snapshotJob{command: command, byID: byID, ids: chunk})
		}
	}
	apply := func(service string, job snapshotJob, data platform.Result, err error) {
		if job.byID == nil {
			for _, position := range job.positions {
				results[position] = readResult(requests[position], data, err)
			}
			return
		}
		rows := map[string]platform.Result{}
		if err == nil {
			for _, row := range resultRows(data) {
				key := "id"
				if service == constants.ServiceInventory {
					key = "sku_id"
				}
				rows[platform.Text(row, key)] = row
			}
		}
		for _, id := range job.ids {
			row, rowErr := rows[id], err
			if rowErr == nil && row == nil {
				rowErr = status.Error(codes.NotFound, "SKU unavailable")
			}
			for _, position := range job.byID[id] {
				results[position] = readResult(requests[position], row, rowErr)
			}
		}
	}
	var workers sync.WaitGroup
	semaphore := make(chan struct{}, 8)
	acquire := func() bool {
		select {
		case semaphore <- struct{}{}:
			return true
		case <-ctx.Done():
			return false
		}
	}
	readLocal := func(service string) {
		defer workers.Done()
		batch := []platform.Command{}
		local := []snapshotJob{}
		for _, job := range jobs[service] {
			if platform.LocalReadAllowed(service, job.command.Action) {
				local = append(local, job)
				batch = append(batch, job.command)
			}
		}
		if len(batch) == 0 {
			return
		}
		if !acquire() {
			for _, job := range local {
				apply(service, job, nil, status.FromContextError(ctx.Err()).Err())
			}
			return
		}
		defer func() { <-semaphore }()
		data, err := g.readRPC(
			ctx,
			service,
			platform.Command{
				Action: constants.ActionReadSnapshot,
				ID:     service,
				Data:   map[string]any{"requests": batch},
			},
		)
		replies := []platform.Result{}
		if err == nil {
			var value struct {
				Results []platform.Result `json:"results"`
			}
			err = platform.Decode(data, &value)
			replies = value.Results
			if err == nil && len(replies) != len(local) {
				err = status.Error(codes.Internal, "incomplete local snapshot")
			}
		}
		for i, job := range local {
			var value platform.Result
			childErr := err
			if childErr == nil {
				reply := replies[i]
				if reply["error"] != nil {
					detail := platform.Map(reply["error"])
					childErr = status.Error(codes.Code(platform.Int(detail, "code")), platform.Text(detail, "message"))
				} else {
					value = platform.Map(reply["data"])
				}
			}
			apply(service, job, value, childErr)
		}
	}
	// A derived read is a distinct fixed computation, not an entity list. More
	// than one different request of the same kind must use its direct endpoint.
	readDerived := func(service, action string) {
		defer workers.Done()
		matches := []snapshotJob{}
		for _, job := range jobs[service] {
			if job.command.Action == action && !platform.LocalReadAllowed(service, action) {
				matches = append(matches, job)
			}
		}
		if len(matches) == 0 {
			return
		}
		if len(matches) > 1 {
			for _, job := range matches {
				apply(
					service,
					job,
					nil,
					status.Error(codes.InvalidArgument, "multiple derived reads must use direct endpoints"),
				)
			}
			return
		}
		job := matches[0]
		if !acquire() {
			apply(service, job, nil, status.FromContextError(ctx.Err()).Err())
			return
		}
		defer func() { <-semaphore }()
		data, err := g.readRPC(ctx, service, job.command)
		apply(service, job, data, err)
	}
	workers.Add(34)
	go readLocal(constants.ServiceAddress)
	go readLocal(constants.ServiceAftersale)
	go readLocal(constants.ServiceAnalytics)
	go readLocal(constants.ServiceCampaign)
	go readLocal(constants.ServiceCart)
	go readLocal(constants.ServiceCatalog)
	go readLocal(constants.ServiceContent)
	go readLocal(constants.ServiceCoupon)
	go readLocal(constants.ServiceEngagement)
	go readLocal(constants.ServiceFlash)
	go readLocal(constants.ServiceFulfillment)
	go readLocal(constants.ServiceGroup)
	go readLocal(constants.ServiceHelpdesk)
	go readLocal(constants.ServiceInteraction)
	go readLocal(constants.ServiceInventory)
	go readLocal(constants.ServiceInvoice)
	go readLocal(constants.ServiceLogistics)
	go readLocal(constants.ServiceMember)
	go readLocal(constants.ServiceNotification)
	go readLocal(constants.ServiceOrder)
	go readLocal(constants.ServicePayment)
	go readLocal(constants.ServicePoints)
	go readLocal(constants.ServicePricing)
	go readLocal(constants.ServiceReconciliation)
	go readLocal(constants.ServiceRefund)
	go readLocal(constants.ServiceSearch)
	go readDerived(constants.ServiceCoupon, constants.ActionTemplates)
	go readDerived(constants.ServiceCoupon, constants.ActionTemplatesSnapshot)
	go readDerived(constants.ServiceHelpdesk, constants.ActionFaq)
	go readDerived(constants.ServiceInteraction, constants.ActionList)
	go readDerived(constants.ServiceRecommendation, constants.ActionList)
	go readDerived(constants.ServiceSearch, constants.ActionQuery)
	go readDerived(constants.ServiceMedia, constants.ActionGet)
	go readDerived(constants.ServiceMedia, constants.ActionList)
	workers.Wait()
	// Unsupported kinds cannot become an arbitrary fallback RPC loop.
	for i, item := range requests {
		if results[i] == nil {
			results[i] = readResult(item, nil, status.Error(codes.PermissionDenied, "unsupported snapshot read"))
		}
	}

	return results
}
func (g *gateway) readRPC(ctx context.Context, service string, c platform.Command) (platform.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	return g.rpc.Call(ctx, service, c)
}
func resultRows(result platform.Result) []platform.Result {
	if result == nil {
		return nil
	}
	// RPC and in-process test callers may use different slice element types.
	b, _ := json.Marshal(result["items"])
	var rows []platform.Result
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	_ = decoder.Decode(&rows)
	return rows
}
