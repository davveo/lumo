package usecase

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/democatalog"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (platform.Result, error) {
	ctx, span := platform.Start(ctx, "catalog.usecase."+c.Action)
	defer span.End()
	switch c.Action {
	case "list":
		st := "ACTIVE"
		if include, _ := c.Data["include_disabled"].(bool); c.Role == "admin" && include {
			st = ""
		}
		vs, total, err := s.repo.ListProducts(ctx, st, int(platform.Int(c.Data, "limit")), int(platform.Int(c.Data, "offset")))
		if err != nil {
			return nil, err
		}
		items := make([]platform.Result, 0, len(vs))
		for _, v := range vs {
			items = append(items, view(v))
		}
		return platform.Result{"items": items, "total": total}, nil
	case "get":
		if c.ID == "" {
			return nil, bad("sku ID required")
		}
		r, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if r.Status != "ACTIVE" && c.Role != "admin" {
			return nil, status.Error(codes.NotFound, "SKU unavailable")
		}
		return view(r), nil
	case "upsert":
		if e := platform.RequireAdmin(c); e != nil {
			return nil, e
		}
		name := platform.Text(c.Data, "name")
		price := platform.Int(c.Data, "price_cents")
		if c.ID == "" || len(c.ID) > 128 || name == "" || price < 1 || price > 1000000000 {
			return nil, bad("id, name and positive price_cents required")
		}
		active, ok := c.Data["active"].(bool)
		if !ok {
			active = true
		}
		st := "ACTIVE"
		if !active {
			st = "DISABLED"
		}
		rows, e := catalogRows(c.ID, c.Data, st)
		if e != nil {
			return nil, e
		}
		evt := platform.Event{ID: platform.ID("evt"), Type: "ProductChanged", AggregateID: c.ID, UserID: c.UserID, Data: view(rows[0])}
		if e = s.repo.SaveProduct(ctx, c.ID, rows, &evt); e != nil {
			return nil, e
		}
		r, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		return view(r), nil
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported catalog action")
	}
}
func (s *Service) seed(ctx context.Context) error {
	if !demo() {
		return nil
	}
	// Legacy IDs remain readable for existing development orders; the storefront
	// lists only product/default SKUs with their independently priced variants.
	for _, p := range []struct {
		ID, Name, Brand, Category string
		Price                     int64
	}{
		{"sku-phone", "iPhone 16 Pro", "Apple", "手机数码", 699900},
		{"sku-headphones", "无线降噪耳机", "Sony", "手机数码", 129900},
		{"sku-watch", "智能运动手表", "LUMO", "服饰箱包", 114900},
		{"sku-coffee", "全自动咖啡机", "LUMO", "家用电器", 39900},
	} {
		r := record(p.ID, "", "sku", "ACTIVE", map[string]any{"name": p.Name, "price_cents": p.Price, "brand": p.Brand, "category": p.Category, "active": true, "catalog_visible": false})
		aliases := map[string]string{"sku-phone": "iphone", "sku-headphones": "headphones", "sku-watch": "watch", "sku-coffee": "coffee"}
		if alias := aliases[p.ID]; alias != "" {
			d := payload(r)
			d["product_id"] = alias
			for _, product := range democatalog.Products() {
				if product["id"] == alias {
					d["image"] = product["image"]
					d["color"] = product["colors"].([]any)[0]
					d["option"] = product["defaultOption"]
				}
			}
			r = record(p.ID, "", "sku", "ACTIVE", d)
		}
		r.ParentID = "__legacy__"
		inserted, e := s.repo.InsertOnce(ctx, &r, nil)
		if e != nil {
			return e
		}
		if !inserted {
			old, e := s.repo.Get(ctx, r.ID)
			if e != nil {
				return e
			}
			d := payload(old)
			for k, v := range payload(r) {
				if _, ok := d[k]; !ok {
					d[k] = v
				}
			}
			updated := record(old.ID, old.UserID, old.Kind, old.Status, d)
			updated.ParentID = "__legacy__"
			if e = s.repo.Save(ctx, &updated, nil); e != nil {
				return e
			}
		}
	}
	for _, p := range democatalog.Products() {
		p["price_cents"] = int64(math.Round(p["price"].(float64) * 100))
		rows, e := catalogRows(p["id"].(string), p, "ACTIVE")
		if e != nil {
			return e
		}
		for i := range rows {
			if _, e = s.repo.InsertOnce(ctx, &rows[i], nil); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Service) event(context.Context, platform.Event) error { return nil }

// catalogRows materializes each selectable option as an authoritative SKU. The
// product/default SKU carries presentation metadata and the complete variant map.
func catalogRows(id string, data map[string]any, state string) ([]domain.Record, error) {
	var in struct {
		Colors  []string  `json:"colors"`
		Options []string  `json:"options"`
		Default string    `json:"defaultOption"`
		Prices  []float64 `json:"prices"`
	}
	if e := platform.Decode(data, &in); e != nil {
		return nil, bad("invalid variant presentation")
	}
	if len(in.Colors) == 0 {
		in.Colors = []string{"经典款"}
	}
	if len(in.Options) == 0 {
		in.Options = []string{"标准版"}
	}
	if len(in.Colors) > 12 || len(in.Options) > 12 {
		return nil, bad("at most 12 colors/options allowed")
	}
	if in.Default == "" {
		in.Default = in.Options[0]
	}
	defaultIndex := -1
	seen := map[string]bool{}
	for i, o := range in.Options {
		if o == "" || len(o) > 200 || seen[o] {
			return nil, bad("invalid or duplicate option")
		}
		seen[o] = true
		if o == in.Default {
			defaultIndex = i
		}
	}
	if defaultIndex < 0 {
		return nil, bad("defaultOption must be a selectable option")
	}
	seen = map[string]bool{}
	for _, c := range in.Colors {
		if c == "" || len(c) > 200 || seen[c] {
			return nil, bad("invalid or duplicate color")
		}
		seen[c] = true
	}
	base := platform.Int(data, "price_cents")
	prices := make([]int64, len(in.Options))
	for i := range prices {
		prices[i] = base
	}
	if len(in.Prices) > 0 {
		if len(in.Prices) != len(in.Options) {
			return nil, bad("prices must match options")
		}
		offset := base - int64(math.Round(in.Prices[defaultIndex]*100))
		for i, p := range in.Prices {
			if math.IsNaN(p) || math.IsInf(p, 0) {
				return nil, bad("invalid variant price")
			}
			prices[i] = int64(math.Round(p*100)) + offset
			if prices[i] < 1 || prices[i] > 1000000000 {
				return nil, bad("variant price outside allowed range")
			}
		}
	}
	attrs := map[string]any{}
	for _, k := range []string{"name", "brand", "category", "description", "sub", "image", "old", "sales", "label", "highlights", "highSubs", "specs", "image_file_id"} {
		if v, ok := data[k]; ok {
			attrs[k] = v
		}
	}
	attrs["colors"] = in.Colors
	attrs["options"] = in.Options
	attrs["defaultOption"] = in.Default
	presentationPrices := make([]float64, len(prices))
	for i, v := range prices {
		presentationPrices[i] = float64(v) / 100
	}
	attrs["prices"] = presentationPrices
	variants := []map[string]any{}
	rows := []domain.Record{}
	for ci, color := range in.Colors {
		for oi, option := range in.Options {
			sku := id
			if ci != 0 || oi != defaultIndex {
				sku = fmt.Sprintf("%s--%d-%d", id, ci, oi)
			}
			v := map[string]any{"sku_id": sku, "color": color, "option": option, "price_cents": prices[oi]}
			variants = append(variants, v)
			p := map[string]any{}
			for k, v := range attrs {
				p[k] = v
			}
			p["product_id"] = id
			p["color"] = color
			p["option"] = option
			p["price_cents"] = prices[oi]
			p["active"] = state == "ACTIVE"
			p["catalog_visible"] = sku == id
			r := record(sku, "", "sku", state, p)
			if sku != id {
				r.ParentID = id
				rows = append(rows, r)
			} else {
				rows = append([]domain.Record{r}, rows...)
			}
		}
	}
	p := payload(rows[0])
	p["variants"] = variants
	rows[0] = record(id, "", "sku", state, p)
	return rows, nil
}
