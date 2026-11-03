package usecase

import "strings"

func correctQuery(query string, custom map[string]string) string {
	key := strings.ToLower(strings.TrimSpace(query))
	aliases := map[string]string{
		"iphnoe": "iphone",
		"ipone":  "iphone",
		"苹果手机":   "iphone",
		"苹果电脑":   "macbook",
		"蓝牙耳麦":   "耳机",
		"运动球鞋":   "运动鞋",
		"咖啡":     "咖啡",
		"笔记本电脑":  "MacBook",
	}
	for a, b := range custom {
		aliases[strings.ToLower(strings.TrimSpace(a))] = strings.TrimSpace(b)
	}
	if v := aliases[key]; v != "" {
		return v
	}
	// Correct only close, unambiguous known brand/model terms. Never rewrite a
	// long arbitrary sentence or silently chain a user-configured alias.
	if len(key) < 4 || len(key) > 16 {
		return query
	}
	hit := ""
	for _, term := range []string{"iphone", "macbook", "chanel", "sony", "nike"} {
		if near(key, term) {
			if hit != "" {
				return query
			}
			hit = term
		}
	}
	if hit != "" {
		return hit
	}
	return query
}
func near(a, b string) bool {
	if a == b {
		return false
	}
	x, y := []rune(a), []rune(b)
	if len(x) != len(y) {
		return false
	}
	diff := []int{}
	for i := range x {
		if x[i] != y[i] {
			diff = append(diff, i)
		}
	}
	return len(diff) == 1 ||
		(len(diff) == 2 && diff[1] == diff[0]+1 && x[diff[0]] == y[diff[1]] && x[diff[1]] == y[diff[0]])
}
