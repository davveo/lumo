// Package fulfillmentplan defines immutable line quantities exchanged between
// order, warehouse and inventory services. It contains no persistence or RPC.
package fulfillmentplan

import (
	"encoding/json"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sort"
)

type Line struct {
	SKUID    string `json:"sku_id"`
	Quantity int64  `json:"quantity"`
}
type Progress struct {
	SKUID       string `json:"sku_id"`
	Quantity    int64  `json:"quantity"`
	Cancelled   int64  `json:"cancelled"`
	Dispatching int64  `json:"dispatching"`
	Shipped     int64  `json:"shipped"`
	Delivered   int64  `json:"delivered"`
	Held        int64  `json:"held"`
	Planned     int64  `json:"planned"`
	Remaining   int64  `json:"remaining"`
}

func Normalize(value any) ([]Line, string, error) {
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, "", e
	}
	var input []Line
	if json.Unmarshal(raw, &input) != nil || len(input) < 1 || len(input) > 100 {
		return nil, "", status.Error(codes.InvalidArgument, "请选择1至100个商品行")
	}
	counts := map[string]int64{}
	for _, l := range input {
		if l.SKUID == "" || len(l.SKUID) > 80 || l.Quantity < 1 || l.Quantity > 100000 {
			return nil, "", status.Error(codes.InvalidArgument, "商品编号或数量无效")
		}
		counts[l.SKUID] += l.Quantity
		if counts[l.SKUID] > 100000 {
			return nil, "", status.Error(codes.InvalidArgument, "商品数量超出范围")
		}
	}
	lines := make([]Line, 0, len(counts))
	for k, n := range counts {
		lines = append(lines, Line{SKUID: k, Quantity: n})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].SKUID < lines[j].SKUID })
	raw, _ = json.Marshal(lines)
	return lines, string(raw), nil
}
func Parse(raw string) ([]Line, error) {
	var v []Line
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil, status.Error(codes.Internal, "订单行快照不可用")
	}
	lines, _, e := Normalize(v)
	return lines, e
}
