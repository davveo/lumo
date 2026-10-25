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
	if adminActions[key] && principal.Role != constants.RoleAdmin {
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
	jobs := []func(){}
	for _, positions := range duplicates {
		positions := positions
		jobs = append(jobs, func() {
			item := requests[positions[0]]
			command := platform.Command{Action: item.Action, ID: item.Command.ID, Data: item.Command.Data}
			if item.auth {
				command.UserID, command.Role = principal.UserID, principal.Role
			}
			data, err := g.readRPC(ctx, item.Service, command)
			for _, position := range positions {
				results[position] = readResult(requests[position], data, err)
			}
		})
	}
	for _, positions := range bulk {
		// The public batch is at most 64 entries; bootstrap can have several
		// hundred variants, so split into bounded database/RPC reads.
		byID, ids := map[string][]int{}, []string{}
		for _, position := range positions {
			id := requests[position].Command.ID
			if _, exists := byID[id]; !exists {
				ids = append(ids, id)
			}
			byID[id] = append(byID[id], position)
		}
		for first := 0; first < len(ids); first += 500 {
			last := min(first+500, len(ids))
			chunk := append([]string(nil), ids[first:last]...)
			item, byID := requests[positions[0]], byID
			jobs = append(jobs, func() {
				command := platform.Command{Action: constants.ActionBatchGet, Data: map[string]any{"ids": chunk}}
				if item.auth {
					command.UserID, command.Role = principal.UserID, principal.Role
				}
				data, err := g.readRPC(ctx, item.Service, command)
				rows := map[string]platform.Result{}
				if err == nil {
					for _, row := range resultRows(data) {
						id := platform.Text(row, "id")
						if item.Service == constants.ServiceInventory {
							id = platform.Text(row, "sku_id")
						}
						rows[id] = row
					}
				}
				for _, id := range chunk {
					row, rowErr := rows[id], err
					if rowErr == nil && row == nil {
						rowErr = status.Error(codes.NotFound, "SKU unavailable")
					}
					for _, position := range byID[id] {
						results[position] = readResult(requests[position], row, rowErr)
					}
				}
			})
		}
	}
	queue := make(chan func(), len(jobs))
	for _, job := range jobs {
		queue <- job
	}
	close(queue)
	var workers sync.WaitGroup
	for i := 0; i < min(8, len(jobs)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range queue {
				job()
			}
		}()
	}
	workers.Wait()
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
