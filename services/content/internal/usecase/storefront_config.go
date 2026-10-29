package usecase

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var storefrontAsset = regexp.MustCompile(`^(?:assets/)?(?:[A-Za-z0-9_-]+\.(?:jpg|jpeg|png|webp)|demo-[a-z0-9-]+\.svg)$`)
var storefrontInlineImage = regexp.MustCompile(`^data:image/(?:png|jpeg|webp);base64,[A-Za-z0-9+/=]+$`)

func validStorefrontImage(value string) bool {
	if storefrontAsset.MatchString(value) || storefrontInlineImage.MatchString(value) {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

// Validate fields owned by the storefront settings editor, preserving unrelated
// configuration keys used by other editors. No HTML or executable links enter
// the presentation fields.
func validateStorefrontSettings(body string) error {
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &raw) != nil || raw == nil {
		return bad("storefront settings must be a JSON object")
	}
	limits := map[string]int{
		"headerSlogan":      60,
		"brandTagline":      40,
		"searchPlaceholder": 40,
		"heroButton":        20,
		"heroLink":          200,
		"announcement":      120,
		"announcementLink":  200,
		"footerText":        160,
		"contactPhone":      30,
		"contactHours":      50,
	}
	for key, limit := range limits {
		value, ok := raw[key]
		if !ok {
			continue
		}
		var text string
		if json.Unmarshal(value, &text) != nil || utf8.RuneCountInString(text) > limit ||
			strings.ContainsRune(text, '\x00') {
			return bad("invalid storefront text: " + key)
		}
		if (key == "heroLink" || key == "announcementLink") && !popupTargetPattern.MatchString(text) {
			return bad("storefront link must target an allowed shopping page")
		}
	}
	for _, key := range []string{"showCategories", "showPicks", "showMember", "showCoupons", "showActivities", "showRecommendations"} {
		if value, ok := raw[key]; ok {
			var enabled bool
			if json.Unmarshal(value, &enabled) != nil || string(value) == "null" {
				return bad("invalid storefront module flag")
			}
		}
	}
	if value, ok := raw["heroInterval"]; ok {
		var interval int
		if json.Unmarshal(value, &interval) != nil || (interval != 0 && (interval < 3 || interval > 30)) {
			return bad("hero interval must be 0 or 3..30 seconds")
		}
	}
	if value, ok := raw["heroSlides"]; ok {
		var slides []struct {
			Title    string `json:"title"`
			Subtitle string `json:"subtitle"`
			Eyebrow  string `json:"eyebrow"`
			Image    string `json:"image"`
			Button   string `json:"button"`
			Link     string `json:"link"`
		}
		if json.Unmarshal(value, &slides) != nil || len(slides) > 4 || string(value) == "null" {
			return bad("at most 4 additional hero slides allowed")
		}
		for _, slide := range slides {
			if strings.TrimSpace(slide.Title) == "" || utf8.RuneCountInString(slide.Title) > 60 ||
				utf8.RuneCountInString(slide.Subtitle) > 120 ||
				utf8.RuneCountInString(slide.Eyebrow) > 80 ||
				slide.Button == "" ||
				utf8.RuneCountInString(slide.Button) > 20 ||
				!validStorefrontImage(slide.Image) ||
				!popupTargetPattern.MatchString(slide.Link) {
				return bad("invalid storefront slide text, image or internal link")
			}
		}
	}
	return nil
}
