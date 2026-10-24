package democatalog

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// The seed fixture is derived from the content service's initial navigation. It
// supplies demo products, never a navigation fallback or runtime configuration.
//
//go:generate go run ./generate -source ../../services/content/internal/usecase/category_navigation.json -out assortment.json
//go:embed assortment.json
var assortmentJSON []byte

type assortmentFixture struct {
	SourceSHA256 string           `json:"source_sha256"`
	Terms        []assortmentTerm `json:"terms"`
}
type assortmentTerm struct {
	Group    string   `json:"group"`
	Category string   `json:"category"`
	Section  string   `json:"section"`
	Label    string   `json:"label"`
	Query    string   `json:"query"`
	Bundle   bool     `json:"bundle,omitempty"`
	Brands   []string `json:"brands,omitempty"`
}
type assortmentNavigation struct {
	Version int `json:"version"`
	Groups  []struct {
		ID       string `json:"id"`
		Category string `json:"category"`
		Rows     []struct {
			Title string `json:"title"`
			Query string `json:"query"`
			Items []struct {
				Label string `json:"label"`
				Query string `json:"query"`
			} `json:"items"`
		} `json:"rows"`
		Shortcuts []struct {
			Label string `json:"label"`
			Query string `json:"query"`
		} `json:"shortcuts"`
		Brands []struct {
			Brand string `json:"brand"`
		} `json:"brands"`
	} `json:"groups"`
}

// BuildAssortmentFixture derives a compact demo-product plan from a backend
// navigation document. It covers independent row/shortcut queries with themed
// bundles instead of adding unrelated keywords to every leaf product's title.
func BuildAssortmentFixture(raw []byte) ([]byte, error) {
	var navigation assortmentNavigation
	if err := json.Unmarshal(raw, &navigation); err != nil {
		return nil, fmt.Errorf("decode category navigation: %w", err)
	}
	if navigation.Version != 1 || navigation.Groups == nil {
		return nil, fmt.Errorf("category navigation version 1 and groups required")
	}
	digest := sha256.Sum256(raw)
	fixture := assortmentFixture{SourceSHA256: hex.EncodeToString(digest[:]), Terms: []assortmentTerm{}}
	identities := map[string]bool{}
	for _, group := range navigation.Groups {
		if group.ID == "" || strings.TrimSpace(group.Category) == "" {
			return nil, fmt.Errorf("category group ID and category required")
		}
		brands := []string{}
		for _, brand := range group.Brands {
			if strings.TrimSpace(brand.Brand) != "" {
				brands = append(brands, strings.TrimSpace(brand.Brand))
			}
		}
		start := len(fixture.Terms)
		add := func(section, label, query string, bundle bool) error {
			label, query = strings.TrimSpace(label), strings.TrimSpace(query)
			if label == "" {
				return fmt.Errorf("category %s has an empty product label", group.ID)
			}
			if query == "" {
				query = label
			}
			identity := group.ID + "\x00" + section + "\x00" + label + "\x00" + query
			if identities[identity] {
				return fmt.Errorf("duplicate demo product term in category %s: %s", group.ID, query)
			}
			identities[identity] = true
			fixture.Terms = append(fixture.Terms, assortmentTerm{Group: group.ID, Category: group.Category, Section: section, Label: label, Query: query, Bundle: bundle, Brands: brands})
			return nil
		}
		for _, row := range group.Rows {
			query := strings.TrimSpace(row.Query)
			if query == "" {
				query = row.Title
			}
			covered := containsFold(group.Category, query)
			for _, item := range row.Items {
				if err := add(row.Title, item.Label, item.Query, false); err != nil {
					return nil, err
				}
				if containsFold(item.Label, query) || containsFold(item.Query, query) {
					covered = true
				}
			}
			if !covered {
				if err := add(row.Title, row.Title, query, true); err != nil {
					return nil, err
				}
			}
		}
		for _, shortcut := range group.Shortcuts {
			query := strings.TrimSpace(shortcut.Query)
			if query == "" || containsFold(group.Category, query) {
				continue
			}
			covered := false
			for _, term := range fixture.Terms[start:] {
				if containsFold(term.Query, query) || containsFold(term.Label, query) {
					covered = true
					break
				}
			}
			if !covered {
				if err := add("快捷精选", shortcut.Label, query, true); err != nil {
					return nil, err
				}
			}
		}
	}
	return json.MarshalIndent(fixture, "", "  ")
}

// ExpandedProducts returns fresh, deterministically identified demo products.
// The original twelve products intentionally remain in Products, so existing
// orders, promotions, fixtures and caller choices retain their exact behavior.
func ExpandedProducts() []map[string]any {
	var fixture assortmentFixture
	if err := json.Unmarshal(assortmentJSON, &fixture); err != nil {
		panic("invalid embedded demo assortment: " + err.Error())
	}
	return expandAssortment(fixture)
}

// ProductsFromNavigation generates the same assortment from a current backend
// configuration, useful for an explicitly requested demo-data import.
func ProductsFromNavigation(raw []byte) ([]map[string]any, error) {
	encoded, err := BuildAssortmentFixture(raw)
	if err != nil {
		return nil, err
	}
	var fixture assortmentFixture
	if err = json.Unmarshal(encoded, &fixture); err != nil {
		return nil, err
	}
	return expandAssortment(fixture), nil
}

// ExpandedSKUs matches the catalog's deterministic color/option materialization.
func ExpandedSKUs() []string { return productSKUs(ExpandedProducts()) }

func containsFold(text, query string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(query))
}

func expandAssortment(fixture assortmentFixture) []map[string]any {
	products := make([]map[string]any, 0, len(fixture.Terms)*3)
	for _, term := range fixture.Terms {
		count := 3
		if term.Bundle {
			count = 1
		}
		for edition := 0; edition < count; edition++ {
			identity := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", term.Group, term.Section, term.Label, term.Query, edition)
			digest := sha256.Sum256([]byte(identity))
			id := "demo-assortment-" + hex.EncodeToString(digest[:10])
			number := int(digest[0])*256 + int(digest[1])
			brand := assortmentBrand(term, edition)
			subject := term.Label
			if !containsFold(subject, term.Query) {
				subject = term.Query
			}
			suffix := []string{"轻享款", "精选款", "进阶款"}[edition]
			if term.Bundle {
				suffix = "精选组合"
			}
			name := brand + " " + subject + " " + suffix
			price := assortmentPrice(term, edition, number)
			options := []any{"标准装", "升级装"}
			if term.Bundle {
				options = []any{"基础组合", "丰富组合"}
			}
			extra := max(5, price/5)
			products = append(products, map[string]any{
				"id": id, "name": name, "sub": term.Section + " · 演示商品，便于体验搜索与购买流程", "category": term.Category,
				"image": assortmentImage(term), "price": float64(price), "old": float64(price + max(10, price/4)),
				"sales": number%950 + 20, "label": []string{"新品精选", "日常好物", "品质之选"}[edition],
				"colors": []any{"经典款"}, "options": options, "defaultOption": options[0], "prices": []any{float64(price), float64(price + extra)},
				"highlights": []any{term.Label, term.Section, "两种规格", "演示商品"},
				"highSubs":   []any{"按需选择", "分类精选", "独立库存", "用于功能体验"},
				"specs":      []any{[]any{"商品名称", name}, []any{"所属品类", term.Category}, []any{"细分类目", term.Label}, []any{"商品性质", "演示商品，名称、价格、销量及库存仅供功能体验"}},
				"brand":      brand, "stock": 50, "visible": true, "active": true, "demo": true, "demo_source": "category-assortment-v1",
			})
		}
	}
	return products
}

func assortmentBrand(term assortmentTerm, edition int) string {
	// Keep recognizable configured brands on compatible product families;
	// generic household/office bundles use our explicitly synthetic series.
	compatible := func(brand string) bool {
		q := strings.ToLower(term.Query)
		switch strings.ToLower(brand) {
		case "apple":
			return strings.Contains(q, "iphone") || strings.Contains(q, "笔记本") && term.Section != "文具耗材" || strings.Contains(q, "平板电脑") || strings.Contains(q, "一体机") || strings.Contains(q, "充电器") || strings.Contains(q, "数据线")
		case "sony":
			return strings.Contains(q, "耳机") || strings.Contains(q, "相机") || strings.Contains(q, "镜头") || strings.Contains(q, "音箱") || strings.Contains(q, "游戏手机")
		case "chanel":
			return term.Section == "彩妆香氛" || strings.Contains(q, "香水")
		case "nike":
			return term.Section == "运动鞋服"
		default:
			return true
		}
	}
	if strings.Contains(strings.ToLower(term.Query), "iphone") {
		for _, brand := range term.Brands {
			if strings.EqualFold(brand, "Apple") {
				return brand
			}
		}
	}
	brands := []string{}
	for _, brand := range term.Brands {
		if compatible(brand) {
			brands = append(brands, brand)
		}
	}
	if len(brands) == 0 {
		return "LUMO"
	}
	return brands[edition%len(brands)]
}

func assortmentImage(term assortmentTerm) string {
	q := strings.ToLower(term.Query)
	has := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(q, word) {
				return true
			}
		}
		return false
	}
	if term.Bundle {
		return "assets/demo-" + term.Group + ".svg"
	}
	switch {
	case strings.Contains(q, "iphone"):
		return "iphone-black.png"
	case term.Group == "digital" && has("安卓手机", "折叠屏手机", "老人手机", "游戏手机"):
		return "phone-pro.jpg"
	case term.Group == "computers" && term.Section == "电脑整机" && has("笔记本", "轻薄本", "游戏本"):
		return "laptop.jpg"
	case has("耳机"):
		return "headphones.jpg"
	case has("音箱"):
		return "speaker.jpg"
	case has("相机") && !has("相机包"):
		return "camera.jpg"
	case q == "沙发":
		return "sofa.jpg"
	case has("咖啡豆", "挂耳咖啡", "速溶咖啡"):
		return "coffee.jpg"
	case has("运动鞋", "跑步鞋", "篮球鞋", "训练鞋"):
		return "sneakers.jpg"
	case has("背包", "手提包", "托特包", "旅行包"):
		return "bag.jpg"
	case q == "香水":
		return "perfume.jpg"
	case has("手表", "腕表") && !has("配件", "充电器", "表带"):
		return "watch.jpg"
	case has("冰箱") && !has("收纳", "配件"):
		return "appliance.jpg"
	default:
		return "assets/demo-" + term.Group + ".svg"
	}
}

type priceBand struct{ minimum, maximum int }

func assortmentPrice(term assortmentTerm, edition, entropy int) int {
	bands := map[string]priceBand{
		"appliances": {999, 5999}, "computers": {129, 899}, "digital": {99, 1299}, "small-appliances": {99, 899}, "food": {19, 129}, "beauty": {39, 399},
		"fashion": {69, 399}, "sports": {79, 499}, "home": {49, 599}, "kitchen": {39, 299}, "renovation": {29, 399}, "baby": {39, 399}, "pets": {19, 199},
		"books": {29, 149}, "jewelry": {99, 999}, "shoes": {49, 399}, "automotive": {39, 499}, "industrial": {29, 399}, "garden": {19, 199}, "wellness": {39, 399},
	}
	band, ok := bands[term.Group]
	if !ok {
		band = priceBand{49, 399}
	}
	q := strings.ToLower(term.Query)
	contains := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(q, word) {
				return true
			}
		}
		return false
	}
	switch {
	case term.Group == "appliances" && contains("支架", "遥控器", "挡风板", "收纳", "底座", "滤芯", "线材", "配件"):
		band = priceBand{29, 199}
	case term.Group == "computers" && term.Section == "电脑整机":
		band = priceBand{2599, 8999}
	case term.Group == "computers" && term.Section == "文具耗材":
		band = priceBand{9, 99}
	case term.Group == "digital" && contains("iphone", "手机") && !contains("支架", "壳"):
		band = priceBand{1299, 6999}
	case term.Group == "digital" && contains("相机", "镜头") && !contains("包"):
		band = priceBand{1899, 6999}
	case term.Group == "home" && contains("沙发", "床", "衣柜", "餐桌") && !contains("床上", "床单", "床笠"):
		band = priceBand{699, 3999}
	case term.Group == "books" && contains("钢琴", "电子琴", "吉他"):
		band = priceBand{399, 3999}
	case term.Group == "wellness" && contains("轮椅", "制氧机", "护理床"):
		band = priceBand{799, 3999}
	}
	spread := max(1, band.maximum-band.minimum)
	value := band.minimum + entropy%spread
	return value + edition*max(5, value/10)
}
