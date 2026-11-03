package usecase

import (
	"encoding/json"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/aftersale/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math/big"
	"sort"
)

type preparedClaim struct {
	Purchased       map[string]int64
	Weights         map[string]int64
	Requested       []domain.Line
	Amount, Payable int64
}

func prepareClaim(c platform.Request, o platform.Result, typ string) (preparedClaim, error) {
	var err error
	state := platform.Text(o, "status")
	if state != constants.OrderStatusReadyToFulfill && state != constants.OrderStatusShipped &&
		state != constants.OrderStatusCompleted && state != constants.OrderStatusPartiallyShipped {
		return preparedClaim{}, status.Error(codes.FailedPrecondition, "order is not eligible for aftersale")
	}
	var items []orderLine
	b, _ := json.Marshal(o["items"])
	if err = json.Unmarshal(b, &items); err != nil || len(items) == 0 {
		return preparedClaim{}, status.Error(codes.FailedPrecondition, "order lines unavailable")
	}
	purchased := map[string]int64{}
	weights := map[string]int64{}
	var base int64
	for _, i := range items {
		if i.SKUID == "" || i.Quantity <= 0 {
			return preparedClaim{}, status.Error(codes.FailedPrecondition, "invalid order snapshot")
		}
		purchased[i.SKUID] += i.Quantity
		unit := i.PriceCents
		if unit == 0 {
			unit = i.UnitPriceCents
		}
		line := unit * i.Quantity
		if i.LineTotalCents > 0 {
			line = i.LineTotalCents
		}
		if line <= 0 {
			line = i.Quantity
		}
		weights[i.SKUID] += line
		base += line
	}
	var requested []domain.Line
	if c.Data["lines"] != nil {
		b, _ = json.Marshal(c.Data["lines"])
		if err = json.Unmarshal(b, &requested); err != nil {
			return preparedClaim{}, status.Error(codes.InvalidArgument, "invalid aftersale lines")
		}
	} else {
		for k, q := range purchased {
			requested = append(requested, domain.Line{SKUID: k, Quantity: q})
		}
	}
	if len(requested) == 0 {
		return preparedClaim{}, status.Error(codes.InvalidArgument, "aftersale lines required")
	}
	normalized := map[string]int64{}
	for _, i := range requested {
		if i.Quantity <= 0 || purchased[i.SKUID] == 0 {
			return preparedClaim{}, status.Error(codes.InvalidArgument, "invalid requested quantity")
		}
		normalized[i.SKUID] += i.Quantity
	}
	requested = nil
	for k, q := range normalized {
		if q > purchased[k] {
			return preparedClaim{}, status.Error(codes.FailedPrecondition, "quantity exceeds original purchase")
		}
		requested = append(requested, domain.Line{SKUID: k, Quantity: q})
	}
	sort.Slice(requested, func(i, j int) bool { return requested[i].SKUID < requested[j].SKUID })
	payable := platform.Int(o, "payable_cents")
	var weight int64
	for _, l := range requested {
		weight += weights[l.SKUID] * l.Quantity / purchased[l.SKUID]
	}
	// Prices and line quantities are bounded, but multiplying two valid order
	// amounts can still overflow int64 before the proportional division.
	var allocated big.Int
	allocated.Mul(big.NewInt(payable), big.NewInt(weight))
	allocated.Quo(&allocated, big.NewInt(base))
	amount := allocated.Int64()
	if typ == constants.AftersaleTypeExchange {
		amount = 0
	}
	if typ != constants.AftersaleTypeExchange && amount <= 0 {
		return preparedClaim{}, status.Error(codes.FailedPrecondition, "no refundable cash for requested lines")
	}
	return preparedClaim{
		Purchased: purchased,
		Weights:   weights,
		Requested: requested,
		Amount:    amount,
		Payable:   payable,
	}, nil
}
