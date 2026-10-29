package usecase

import (
	"github.com/davveo/lumo/internal/constants"
	"testing"
)

func TestStorefrontSettingsValidateTypedModulesSlidesAndSafeDestinations(t *testing.T) {
	for _, body := range []string{`{}`, `{"heroInterval":0,"showPicks":false,"future_key":{"preserved":true}}`, `{"heroSlides":[{"title":"精选","image":"hero.jpg","button":"领券","link":"#/coupons"}]}`} {
		if err := validateStorefrontSettings(body); err != nil {
			t.Fatalf("valid settings rejected: %s %v", body, err)
		}
	}
	for _, body := range []string{`not-json`, `[]`, `{"showPicks":"false"}`, `{"heroInterval":1}`, `{"heroLink":"javascript:alert(1)"}`, `{"heroSlides":[{"title":"广告","image":"https://a.invalid/image.jpg","button":"跳转","link":"//attacker.invalid"}]}`, `{"heroSlides":null}`} {
		if err := validateStorefrontSettings(body); err == nil {
			t.Fatalf("unsafe settings accepted: %s", body)
		}
	}
}
func TestPopupLayoutsPreserveTargetingAndValidatePosterImages(t *testing.T) {
	for _, layout := range []string{constants.PopupLayoutCoupon, constants.PopupLayoutAnnouncement, constants.PopupLayoutImage, constants.PopupLayoutWelcome, constants.PopupLayoutMember, constants.PopupLayoutCampaign} {
		p := popupTestConfig("layout")
		p.Content.Layout = layout
		p.Content.ImageURL = "hero.jpg"
		if err := validatePopup(&p); err != nil {
			t.Fatalf("layout %s rejected: %v", layout, err)
		}
	}
	p := popupTestConfig("poster")
	p.Content.Layout = constants.PopupLayoutImage
	if err := validatePopup(&p); err == nil {
		t.Fatal("poster without image accepted")
	}
	p.Content.ImageURL = "javascript:alert(1)"
	if err := validatePopup(&p); err == nil {
		t.Fatal("executable poster URL accepted")
	}
	p.Content.ImageURL = "https://user:secret@example.invalid/image.jpg"
	if err := validatePopup(&p); err == nil {
		t.Fatal("credential URL accepted")
	}
}
