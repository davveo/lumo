package usecase

import "testing"

func TestBoundedCorrectionAndConfiguredAliases(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"iphnoe", "iphone"},
		{"苹果手机", "iphone"},
		{"iphome", "iphone"},
		{"想买个手机", "想买个手机"},
		{"手机壳", "手机壳"},
		{"iphon", "iphon"},
	} {
		if got := correctQuery(tc.in, nil); got != tc.want {
			t.Errorf("%s: %s", tc.in, got)
		}
	}
	if got := correctQuery("苹果手机", map[string]string{"苹果手机": "手机数码"}); got != "手机数码" {
		t.Fatal(got)
	}
}
