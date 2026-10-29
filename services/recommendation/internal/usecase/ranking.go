package usecase

import (
	"github.com/davveo/lumo/internal/constants"
	"github.com/davveo/lumo/services/recommendation/internal/repository"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/davveo/lumo/internal/platform"
)

type product struct {
	id, rawID, name, category, brand string
	price, sales                     float64
	created                          time.Time
	active                           bool
	raw                              map[string]any
	tokens                           map[string]bool
}

func productFrom(row map[string]any) product {
	attrs := platform.Map(row["attributes"])
	text := func(key string) string {
		v := platform.Text(row, key)
		if v == "" {
			v = platform.Text(attrs, key)
		}
		return strings.TrimSpace(v)
	}
	id := text("product_id")
	rawID := text("id")
	if id == "" {
		id = text("parent_id")
	}
	if id == "" {
		id = rawID
	}
	price := float64(platform.Int(row, "price_cents"))
	if price <= 0 {
		price = float64(platform.Int(attrs, "price_cents"))
	}
	sales := float64(platform.Int(row, "sales"))
	if sales <= 0 {
		sales = float64(platform.Int(attrs, "sales"))
	}
	active := true
	if enabled, ok := row["active"].(bool); ok && !enabled {
		active = false
	}
	if text("status") == constants.CatalogStatusDisabled {
		active = false
	}
	if visible, ok := row["catalog_visible"].(bool); ok && !visible {
		active = false
	}
	created, _ := time.Parse(time.RFC3339, text("created_at"))
	return product{
		id:       id,
		rawID:    rawID,
		name:     text("name"),
		category: text("category"),
		brand:    text("brand"),
		price:    price,
		sales:    sales,
		created:  created,
		active:   active,
		raw:      row,
		tokens:   grams(text("name")),
	}
}

type interest struct {
	p      product
	weight float64
	kind   string
}
type queryInterest struct {
	text   string
	weight float64
}
type preference struct {
	interests []interest
	queries   []queryInterest
	purchased map[string]bool
	count     int
}

func signalTime(row map[string]any) time.Time {
	for _, key := range []string{"updated_at", "paid_at", "created_at"} {
		if t, err := time.Parse(time.RFC3339, platform.Text(row, key)); err == nil {
			return t
		}
	}
	return time.Time{}
}
func decay(now, timeAt time.Time, halfLife float64) float64 {
	if timeAt.IsZero() {
		return .65
	}
	age := math.Max(0, now.Sub(timeAt).Hours()/24)
	return math.Exp2(-math.Min(age, 730) / halfLife)
}
func paidOrder(row map[string]any) bool {
	switch platform.Text(row, "status") {
	case constants.OrderStatusPaid,
		constants.OrderStatusPaidConfirming,
		constants.OrderStatusPaidWaitGroup,
		constants.OrderStatusReadyToFulfill,
		constants.OrderStatusShipped,
		constants.OrderStatusCompleted,
		constants.OrderStatusAwaitingGroup,
		constants.OrderStatusWaitingGroup,
		constants.OrderStatusFulfilling:
		return true
	}
	return false
}
func buildProfile(rows signalRows, metadata map[string]product, now time.Time, policies ...repository.Policy) preference {
	policy := repository.DefaultPolicy()
	if len(policies) > 0 {
		policy = policies[0]
	}
	p := preference{purchased: map[string]bool{}}
	interests := map[string]interest{}
	add := func(id, kind string, weight float64, at time.Time) {
		if weight <= 0 {
			return
		}
		v, ok := metadata[id]
		if !ok {
			return
		}
		if parent, exists := metadata[v.id]; exists {
			v = parent
		}
		key := kind + ":" + v.id
		value := interest{v, weight * decay(now, at, 30), kind}
		if prior, ok := interests[key]; !ok || value.weight > prior.weight {
			interests[key] = value
		}
		p.count++
	}
	for _, signal := range []struct {
		rows   []map[string]any
		kind   string
		weight float64
	}{
		{rows.Favorite, constants.RecommendationSignalFavorite, policy.FavoriteWeight},
		{rows.Compare, constants.RecommendationSignalCompare, policy.CompareWeight},
		{rows.History, constants.RecommendationSignalHistory, policy.HistoryWeight},
	} {
		for _, row := range signal.rows {
			add(platform.Text(row, "sku_id"), signal.kind, signal.weight, signalTime(row))
		}
	}
	for _, row := range rows.Search {
		query := strings.TrimSpace(platform.Text(row, "query"))
		if validInterest(query, 80) && len(p.queries) < 20 {
			p.queries = append(p.queries, queryInterest{query, policy.SearchWeight * decay(now, signalTime(row), 14)})
			p.count++
		}
	}
	for _, row := range rows.Orders {
		if !paidOrder(row) {
			continue
		}
		for _, line := range rowsOf(row["items"]) {
			id := platform.Text(line, "sku_id")
			if id == "" {
				continue
			}
			p.purchased[id] = true
			if v, ok := metadata[id]; ok {
				p.purchased[v.id] = true
			}
			add(id, constants.RecommendationSignalPurchase, policy.PurchaseWeight, signalTime(row))
		}
	}
	for _, signal := range interests {
		p.interests = append(p.interests, signal)
	}
	sort.Slice(p.interests, func(i, j int) bool {
		a, b := p.interests[i], p.interests[j]
		if a.weight != b.weight {
			return a.weight > b.weight
		}
		if a.p.id != b.p.id {
			return a.p.id < b.p.id
		}
		return a.kind < b.kind
	})
	if len(p.interests) > 200 {
		p.interests = p.interests[:200]
	}
	return p
}
func validInterest(value string, max int) bool {
	if value == "" || len([]rune(value)) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (p preference) recall(
	contextProduct *product,
	order map[string]any,
	metadata map[string]product,
) ([]string, []string, []string) {
	categories, brands := map[string]float64{}, map[string]float64{}
	add := func(v product, w float64) {
		if validInterest(v.category, 64) {
			categories[v.category] += w
		}
		if validInterest(v.brand, 64) {
			brands[v.brand] += w
		}
	}
	for _, signal := range p.interests {
		add(signal.p, signal.weight)
	}
	if contextProduct != nil {
		add(*contextProduct, 100)
	}
	for _, line := range rowsOf(order["items"]) {
		if v, ok := metadata[platform.Text(line, "sku_id")]; ok {
			add(v, 50)
		}
	}
	top := func(weights map[string]float64, n int) []string {
		keys := []string{}
		for key := range weights {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if weights[keys[i]] != weights[keys[j]] {
				return weights[keys[i]] > weights[keys[j]]
			}
			return keys[i] < keys[j]
		})
		if len(keys) > n {
			keys = keys[:n]
		}
		return keys
	}
	queries, seen := []string{}, map[string]bool{}
	for _, q := range p.queries {
		if !seen[q.text] && len(queries) < 3 {
			queries = append(queries, q.text)
			seen[q.text] = true
		}
	}
	return top(categories, 4), top(brands, 4), queries
}
func same(a, b string) bool { return a != "" && strings.EqualFold(a, b) }
func priceAffinity(a, b float64) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	return math.Exp(-math.Abs(math.Log(a/b)) / .7)
}
func grams(value string) map[string]bool {
	runes := []rune(strings.ToLower(value))
	clean := []rune{}
	for _, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			clean = append(clean, r)
		}
	}
	out := map[string]bool{}
	if len(clean) == 1 {
		out[string(clean)] = true
	}
	for i := 0; i+1 < len(clean); i++ {
		out[string(clean[i:i+2])] = true
	}
	return out
}
func textAffinity(a, b string) float64 {
	x, y := grams(a), grams(b)
	return tokenAffinity(x, y)
}
func tokenAffinity(x, y map[string]bool) float64 {
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	common := 0
	for key := range x {
		if y[key] {
			common++
		}
	}
	return float64(common) / float64(len(x)+len(y)-common)
}
func similarity(a, b product) float64 {
	score := 0.
	if same(a.category, b.category) {
		score += .45
	}
	if same(a.brand, b.brand) {
		score += .2
	}
	score += .2*priceAffinity(a.price, b.price) + .15*tokenAffinity(a.tokens, b.tokens)
	return score
}
func queryAffinity(query string, p product) float64 {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return 0
	}
	text := strings.ToLower(strings.Join([]string{p.name, p.category, p.brand}, " "))
	if strings.Contains(text, q) {
		return 1
	}
	parts := strings.Fields(q)
	matched := 0
	for _, part := range parts {
		if len([]rune(part)) > 1 && strings.Contains(text, part) {
			matched++
		}
	}
	return math.Max(float64(matched)/float64(len(parts)), textAffinity(q, text)*.6)
}

type scoredProduct struct {
	p       product
	score   float64
	reason  string
	reasons []string
}

func rankProducts(
	products []product,
	profile preference,
	contextProduct *product,
	order map[string]any,
	metadata map[string]product,
	scene string,
	limit int,
	now time.Time,
	policies ...repository.Policy,
) []platform.Result {
	policy := repository.DefaultPolicy()
	if len(policies) > 0 {
		policy = policies[0]
	}
	ranked := []scoredProduct{}
	anchors := []product{}
	for _, line := range rowsOf(order["items"]) {
		if p, ok := metadata[platform.Text(line, "sku_id")]; ok {
			anchors = append(anchors, p)
		}
	}
	for index, p := range products {
		// Popularity and freshness are modest exploration factors; the stable
		// source position only breaks otherwise identical cold-start candidates.
		score := policy.PopularityWeight*math.Log1p(math.Max(0, p.sales)) + policy.FreshnessWeight*decay(now, p.created, 45) + .001/float64(index+1)
		reason := "精选在售好物"
		reasons := []string{constants.RecommendationReasonCatalogDiscovery}
		bestWeight := 0.
		weights, total := 0., 0.
		bestKind := ""
		for _, signal := range profile.interests {
			value := similarity(p, signal.p)
			weights += signal.weight * value
			total += signal.weight
			if contribution := signal.weight * value; contribution > bestWeight {
				bestWeight = contribution
				bestKind = signal.kind
			}
		}
		if total > 0 {
			personal := weights / total * math.Min(1, total/8)
			score += personal * policy.PersonalWeight
			if personal > .15 {
				reasons = append(reasons, constants.RecommendationReasonPreferenceAffinity)
				reason = map[string]string{
					constants.RecommendationSignalFavorite: "根据你收藏的好物推荐",
					constants.RecommendationSignalCompare:  "根据你对比过的商品推荐",
					constants.RecommendationSignalHistory:  "根据你近期浏览的偏好推荐",
					constants.RecommendationSignalPurchase: "根据你已购商品的偏好推荐",
				}[bestKind]
			}
		}
		searchScore := 0.
		for _, query := range profile.queries {
			searchScore = math.Max(searchScore, query.weight*queryAffinity(query.text, p))
		}
		if searchScore > .5 {
			score += searchScore
			reasons = append(reasons, constants.RecommendationReasonRecentSearch)
			reason = "与你近期搜索的兴趣相关"
		}
		if contextProduct != nil {
			similar := similarity(p, *contextProduct)
			score += similar * policy.SimilarWeight
			if similar > .2 {
				reasons = append(reasons, constants.RecommendationReasonSimilarProduct)
				reason = "与你正在看的商品相似"
			}
		}
		if scene == constants.RecommendationScenePostPurchase && len(anchors) > 0 {
			best := 0.
			for _, anchor := range anchors {
				best = math.Max(best, similarity(p, anchor))
			}
			score += best * 5
			if best > .25 {
				reasons = append(reasons, constants.RecommendationReasonPurchaseRelated)
				reason = "根据本单商品为你推荐"
			}
		}
		if reason == "" {
			reason = "精选在售好物"
		}
		if scene == constants.RecommendationSceneHome {
			for i, id := range policy.PinnedIDs {
				if id == p.id || id == p.rawID {
					score += 1000000 - float64(i)
					reason = "商城精选推荐"
					break
				}
			}
		}
		ranked = append(ranked, scoredProduct{p, score, reason, reasons})
	}
	// Diversity re-ranking reduces repeated categories and brands while retaining
	// strong personal/context matches. There is no random or session-global state.
	out := []platform.Result{}
	categoryCounts, brandCounts := map[string]int{}, map[string]int{}
	for len(out) < limit && len(ranked) > 0 {
		best, bestAdjusted := 0, math.Inf(-1)
		for i, row := range ranked {
			adjusted := row.score - .65*float64(categoryCounts[row.p.category]) - .3*float64(brandCounts[row.p.brand])
			if adjusted > bestAdjusted+1e-9 ||
				(math.Abs(adjusted-bestAdjusted) < 1e-9 && row.p.id < ranked[best].p.id) {
				best, bestAdjusted = i, adjusted
			}
		}
		row := ranked[best]
		ranked = append(ranked[:best], ranked[best+1:]...)
		categoryCounts[row.p.category]++
		brandCounts[row.p.brand]++
		item := platform.Result{}
		for key, value := range row.p.raw {
			item[key] = value
		}
		item["recommendation_reason"] = row.reason
		item["recommendation_reasons"] = row.reasons
		item["recommendation_score"] = math.Round(row.score*10000) / 10000
		out = append(out, item)
	}
	return out
}
