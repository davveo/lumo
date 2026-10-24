package usecase

import (
	"context"
	"fmt"
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/internal/democatalog"
	"github.com/davveo/lumo/internal/platform"
	"github.com/davveo/lumo/services/catalog/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math"
	"strings"
	"time"
)

func (s *Service) Execute(ctx context.Context, c platform.Command) (traceResult platform.Result, traceErr error) {
	ctx, span := platform.Start(ctx, "catalog.usecase."+c.Action)
	defer span.End()
	platform.TraceRequest(ctx, c)
	defer func() { platform.TraceResponse(ctx, traceResult, traceErr) }()
	switch c.Action {
	case constants.ActionSearchProducts:
		return s.searchProducts(ctx, c)
	case constants.ActionRecommendationCandidates:
		return s.recommendationCandidates(ctx, c)
	case constants.ActionBatchGet:
		var input struct {
			IDs []string `json:"ids"`
		}
		if err := platform.Decode(c.Data, &input); err != nil || len(input.IDs) == 0 || len(input.IDs) > 500 {
			return nil, bad("ids must contain between 1 and 500 SKU IDs")
		}
		ids, seen := []string{}, map[string]bool{}
		for _, id := range input.IDs {
			if id == "" || len(id) > 150 || strings.TrimSpace(id) != id {
				return nil, bad("invalid SKU ID")
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		rows, err := s.repo.GetMany(ctx, ids, c.Role == constants.RoleAdmin)
		if err != nil {
			return nil, err
		}
		byID := map[string]domain.Record{}
		for _, row := range rows {
			byID[row.ID] = row
		}
		items, missing := []platform.Result{}, []string{}
		for _, id := range ids {
			if row, exists := byID[id]; exists {
				items = append(items, view(row))
			} else {
				missing = append(missing, id)
			}
		}
		return platform.Result{"items": items, "missing_ids": missing}, nil
	case constants.ActionList:
		st := constants.CatalogStatusActive
		if include, _ := c.Data["include_disabled"].(bool); c.Role == constants.RoleAdmin && include {
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
	case constants.ActionGet:
		if c.ID == "" {
			return nil, bad("sku ID required")
		}
		r, e := s.repo.Get(ctx, c.ID)
		if e != nil {
			return nil, e
		}
		if r.Status != constants.CatalogStatusActive && c.Role != constants.RoleAdmin {
			return nil, status.Error(codes.NotFound, "SKU unavailable")
		}
		return view(r), nil
	case constants.ActionUpsert:
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
		st := constants.CatalogStatusActive
		if !active {
			st = constants.CatalogStatusDisabled
		}
		rows, e := catalogRows(c.ID, c.Data, st)
		if e != nil {
			return nil, e
		}
		evt := platform.Event{ID: platform.ID("evt"), Type: constants.EventProductChanged, AggregateID: c.ID, UserID: c.UserID, Data: view(rows[0])}
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
		r := record(p.ID, "", constants.CatalogKindSku, constants.CatalogStatusActive, map[string]any{"name": p.Name, "price_cents": p.Price, "brand": p.Brand, "category": p.Category, "active": true, "catalog_visible": false})
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
			r = record(p.ID, "", constants.CatalogKindSku, constants.CatalogStatusActive, d)
		}
		r.ParentID = constants.CatalogParentLegacy
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
			updated.ParentID = constants.CatalogParentLegacy
			if e = s.repo.Save(ctx, &updated, nil); e != nil {
				return e
			}
		}
	}
	baseProducts := democatalog.Products()
	baseRows := make([][]domain.Record, 0, len(baseProducts))
	baseIDs := make([]string, 0, len(baseProducts))
	for _, product := range baseProducts {
		product["price_cents"] = int64(math.Round(product["price"].(float64) * 100))
		rows, err := catalogRows(product["id"].(string), product, constants.CatalogStatusActive)
		if err != nil {
			return err
		}
		baseRows = append(baseRows, rows)
		baseIDs = append(baseIDs, rows[0].ID)
	}
	if err := s.repo.InsertSeedProducts(ctx, baseRows); err != nil {
		return err
	}
	// Place new examples immediately behind the original showcase, but ahead
	// of older load-test data. Existing products keep their creation timestamps.
	showcase, err := s.repo.GetMany(ctx, baseIDs, true)
	if err != nil {
		return err
	}
	var earliest time.Time
	for _, product := range showcase {
		if !product.CreatedAt.IsZero() && (earliest.IsZero() || product.CreatedAt.Before(earliest)) {
			earliest = product.CreatedAt
		}
	}
	if earliest.IsZero() {
		earliest = time.Now().UTC()
	}
	created := earliest.Add(-time.Hour)
	expanded := democatalog.ExpandedProducts()
	seedProducts := make([][]domain.Record, 0, len(expanded))
	for _, product := range expanded {
		product["price_cents"] = int64(math.Round(product["price"].(float64) * 100))
		rows, err := catalogRows(product["id"].(string), product, constants.CatalogStatusActive)
		if err != nil {
			return err
		}
		for index := range rows {
			rows[index].CreatedAt, rows[index].UpdatedAt = created, created
		}
		seedProducts = append(seedProducts, rows)
	}
	return s.repo.InsertSeedProducts(ctx, seedProducts)
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
	for _, k := range []string{"name", "brand", "category", "description", "sub", "image", "old", "sales", "label", "highlights", "highSubs", "specs", "image_file_id", "demo", "demo_source"} {
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
			p["active"] = state == constants.CatalogStatusActive
			p["catalog_visible"] = sku == id
			r := record(sku, "", constants.CatalogKindSku, state, p)
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
	rows[0] = record(id, "", constants.CatalogKindSku, state, p)
	return rows, nil
}
