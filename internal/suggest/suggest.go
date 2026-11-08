// Package suggest ranks near-miss spellings so errors can offer a way forward.
package suggest

import (
	"sort"
	"strings"
	"unicode"
)

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// distance is the optimal-string-alignment edit distance (adjacent
// transpositions cost one), which matches how people actually mistype.
func distance(a, b []rune) int {
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}

// Closest returns up to n candidates that plausibly were meant by query,
// best first. Exact matches are excluded: the caller already tried those.
func Closest(query string, candidates []string, n int) []string {
	q := normalize(query)
	if q == "" || n <= 0 {
		return nil
	}
	qr := []rune(q)
	tolerance := min(3, max(1, len(qr)/3))
	type scored struct {
		text  string
		score int
	}
	var found []scored
	seen := map[string]bool{}
	for _, text := range candidates {
		c := normalize(text)
		if c == "" || c == q || seen[text] {
			continue
		}
		score := -1
		switch {
		case len(q) >= 2 && strings.HasPrefix(c, q):
			score = 1
		case len(c) >= 2 && strings.HasPrefix(q, c):
			score = 2
		case len(q) >= 3 && strings.Contains(c, q):
			score = 3
		default:
			cr := []rune(c)
			if d := distance(qr, cr); d <= tolerance {
				score = 4 + d
			} else if len(cr) > len(qr) && tolerance > 1 {
				// Same-length prefix, so "steeering" still finds "steering_angle".
				if d := distance(qr, cr[:len(qr)]); d <= tolerance-1 {
					score = 8 + d
				}
			}
		}
		if score >= 0 {
			seen[text] = true
			found = append(found, scored{text, score})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score < found[j].score
		}
		if len(found[i].text) != len(found[j].text) {
			return len(found[i].text) < len(found[j].text)
		}
		return found[i].text < found[j].text
	})
	if len(found) == 0 {
		return nil
	}
	out := make([]string, 0, min(n, len(found)))
	for i := 0; i < len(found) && i < n; i++ {
		out = append(out, found[i].text)
	}
	return out
}

// Typos returns up to n candidates that differ from query by one substitution,
// insertion, deletion, or adjacent transposition. Prefix matches are excluded
// so longer search words are not labeled as shorter commands.
func Typos(query string, candidates []string, n int) []string {
	q := normalize(query)
	if q == "" || n <= 0 {
		return nil
	}
	qr := []rune(q)
	type scored struct {
		text  string
		score int
	}
	var found []scored
	seen := map[string]bool{}
	for _, text := range candidates {
		c := normalize(text)
		if c == "" || c == q || seen[text] {
			continue
		}
		cr := []rune(c)
		if abs(len(qr)-len(cr)) > 1 {
			continue
		}
		if strings.HasPrefix(q, c) || strings.HasPrefix(c, q) {
			continue
		}
		d := distance(qr, cr)
		if d != 1 {
			continue
		}
		seen[text] = true
		found = append(found, scored{text, d})
	}
	sort.SliceStable(found, func(i, j int) bool {
		if len(found[i].text) != len(found[j].text) {
			return len(found[i].text) < len(found[j].text)
		}
		return found[i].text < found[j].text
	})
	if len(found) == 0 {
		return nil
	}
	out := make([]string, 0, min(n, len(found)))
	for i := 0; i < len(found) && i < n; i++ {
		out = append(out, found[i].text)
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
