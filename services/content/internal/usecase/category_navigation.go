package usecase

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const categoryNavigationID = "storefront-categories"
const categoryNavigationMaxBytes = 256 << 10

// These are initial server-side configuration values, inserted only when the
// document does not exist. Published administrator changes always take priority.
//
//go:embed category_navigation.json
var categoryNavigationDefaults []byte

type categoryNavigation struct {
	Version int             `json:"version"`
	Groups  []categoryGroup `json:"groups"`
}

type categoryGroup struct {
	ID        string             `json:"id"`
	Category  string             `json:"category"`
	Label     string             `json:"label"`
	Title     string             `json:"title"`
	Icon      string             `json:"icon"`
	Enabled   categoryFlag       `json:"enabled,omitzero"`
	Shortcuts []categoryShortcut `json:"shortcuts"`
	Rows      []categoryRow      `json:"rows"`
	Brands    []categoryBrand    `json:"brands"`
}

type categoryShortcut struct {
	Label string `json:"label"`
	Query string `json:"query"`
}

type categoryRow struct {
	Title string         `json:"title"`
	Query string         `json:"query"`
	Items []categoryItem `json:"items"`
}

type categoryItem struct {
	Label    string       `json:"label"`
	Query    string       `json:"query"`
	Featured categoryFlag `json:"featured,omitzero"`
}

// A missing flag has its documented default, but explicit null is not a bool.
// A value field is intentional: encoding/json skips pointer unmarshaling for null.
type categoryFlag struct {
	value   bool
	present bool
}

func (flag *categoryFlag) UnmarshalJSON(raw []byte) error {
	if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
		return fmt.Errorf("category flag must be boolean")
	}
	flag.value, flag.present = bytes.Equal(raw, []byte("true")), true
	return nil
}
func (flag categoryFlag) MarshalJSON() ([]byte, error) { return json.Marshal(flag.value) }
func (flag categoryFlag) IsZero() bool                 { return !flag.present }

type categoryBrand struct {
	Label string `json:"label"`
	Brand string `json:"brand"`
}

var categoryGroupID = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var categoryIcons = map[string]bool{
	"home": true, "bell": true, "search": true, "cart": true,
	"heart": true, "bag": true, "user": true, "arrow": true,
	"chevron": true, "back": true, "check": true, "close": true,
	"trash": true, "minus": true, "plus": true, "shield": true,
	"truck": true, "return": true, "support": true, "filter": true,
	"sort": true, "pin": true, "ticket": true, "coin": true,
	"chip": true, "camera": true, "layers": true, "sparkles": true,
	"info": true, "clock": true, "edit": true,
}

func categoryText(value string, maximum int, required bool) (string, bool) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum || (required && value == "") {
		return "", false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", false
		}
	}
	return value, true
}

// validateCategoryNavigation rejects unexpected fields and malformed types,
// then returns a canonical JSON document whose array ordering is authoritative.
func validateCategoryNavigation(body string) (string, error) {
	if body == "" || len(body) > categoryNavigationMaxBytes || !utf8.ValidString(body) {
		return "", bad("category navigation body must be valid UTF-8 JSON within 256 KiB")
	}
	var navigation categoryNavigation
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&navigation); err != nil {
		return "", bad("invalid category navigation schema")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", bad("category navigation requires one JSON document")
	}
	if navigation.Version != 1 || navigation.Groups == nil || len(navigation.Groups) > 40 {
		return "", bad("category navigation version must be 1 with 0..40 groups")
	}
	ids, categories := map[string]bool{}, map[string]bool{}
	rows, leaves := 0, 0
	for index := range navigation.Groups {
		group := &navigation.Groups[index]
		if !categoryGroupID.MatchString(group.ID) || ids[group.ID] {
			return "", bad("category group IDs must be unique lowercase identifiers within 64 characters")
		}
		ids[group.ID] = true
		var ok bool
		if group.Category, ok = categoryText(group.Category, 80, true); !ok || categories[group.Category] || group.Category == "全部好物" {
			return "", bad("category names must be unique nonempty text within 80 characters")
		}
		categories[group.Category] = true
		if group.Label, ok = categoryText(group.Label, 80, true); !ok {
			return "", bad("category group label must contain 1..80 characters")
		}
		if group.Title, ok = categoryText(group.Title, 80, true); !ok {
			return "", bad("category group title must contain 1..80 characters")
		}
		if !categoryIcons[group.Icon] {
			return "", bad("unsupported category icon")
		}
		if group.Rows == nil || len(group.Rows) > 20 || len(group.Shortcuts) > 8 || len(group.Brands) > 16 {
			return "", bad("category groups require rows with at most 20 rows, 8 shortcuts and 16 brands")
		}
		if group.Shortcuts == nil {
			group.Shortcuts = []categoryShortcut{}
		}
		if group.Brands == nil {
			group.Brands = []categoryBrand{}
		}
		shortcutLabels := map[string]bool{}
		for index := range group.Shortcuts {
			item := &group.Shortcuts[index]
			if item.Label, ok = categoryText(item.Label, 80, true); !ok || shortcutLabels[item.Label] {
				return "", bad("category shortcut labels must be unique text within 80 characters")
			}
			shortcutLabels[item.Label] = true
			if item.Query, ok = categoryText(item.Query, 120, false); !ok {
				return "", bad("category shortcut query must be text within 120 characters")
			}
		}
		rowTitles := map[string]bool{}
		for index := range group.Rows {
			row := &group.Rows[index]
			rows++
			if row.Title, ok = categoryText(row.Title, 80, true); !ok || rowTitles[row.Title] {
				return "", bad("category row titles must be unique text within 80 characters")
			}
			rowTitles[row.Title] = true
			if row.Query, ok = categoryText(row.Query, 120, false); !ok {
				return "", bad("category row query must be text within 120 characters")
			}
			if row.Query == "" {
				row.Query = row.Title
			}
			if row.Items == nil || len(row.Items) > 40 {
				return "", bad("category rows require an items array with at most 40 entries")
			}
			itemLabels := map[string]bool{}
			for index := range row.Items {
				item := &row.Items[index]
				leaves++
				if item.Label, ok = categoryText(item.Label, 80, true); !ok || itemLabels[item.Label] {
					return "", bad("category item labels must be unique text within 80 characters")
				}
				itemLabels[item.Label] = true
				if item.Query, ok = categoryText(item.Query, 120, false); !ok {
					return "", bad("category item query must be text within 120 characters")
				}
				if item.Query == "" {
					item.Query = item.Label
				}
			}
		}
		brandNames := map[string]bool{}
		for index := range group.Brands {
			brand := &group.Brands[index]
			if brand.Label, ok = categoryText(brand.Label, 80, true); !ok {
				return "", bad("category brand label must contain 1..80 characters")
			}
			if brand.Brand, ok = categoryText(brand.Brand, 80, true); !ok || brandNames[brand.Brand] {
				return "", bad("category brands must be unique text within 80 characters")
			}
			brandNames[brand.Brand] = true
		}
	}
	if rows > 400 || leaves > 2000 {
		return "", bad("category navigation supports at most 400 rows and 2000 items")
	}
	canonical, err := json.Marshal(navigation)
	if err != nil {
		return "", bad("cannot encode category navigation")
	}
	return string(canonical), nil
}

func (s *Service) initializeCategoryNavigation(ctx context.Context) error {
	body, err := validateCategoryNavigation(string(categoryNavigationDefaults))
	if err != nil {
		return fmt.Errorf("embedded category navigation: %w", err)
	}
	row := record(categoryNavigationID, "", "storefront", "PUBLISHED", map[string]any{
		"title": "商城分类导航", "body": body, "version": time.Now().Unix(),
	})
	_, err = s.repo.InsertOnce(ctx, &row, nil)
	return err
}
