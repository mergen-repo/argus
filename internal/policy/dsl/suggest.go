package dsl

import "strings"

// Suggest returns the closest string in 'candidates' to 'input', if the
// edit distance is <= maxDist (typically 2). Returns "" when no candidate
// is close enough. Used for "did you mean X?" error messages.
//
// FIX-243 Wave A — DSL parser polish.
func Suggest(input string, candidates []string, maxDist int) string {
	if input == "" || len(candidates) == 0 || maxDist < 0 {
		return ""
	}
	best := ""
	bestDist := maxDist + 1
	lowIn := strings.ToLower(input)
	for _, c := range candidates {
		d := levenshtein(lowIn, strings.ToLower(c))
		if d < bestDist {
			best = c
			bestDist = d
		}
	}
	if bestDist <= maxDist {
		return best
	}
	return ""
}

// levenshtein computes the standard edit distance between a and b using
// a two-row DP, O(len(a)*len(b)) time, O(min) space.
func levenshtein(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	la := len(ar)
	lb := len(br)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// Ensure b is the shorter row to minimize allocation.
	if lb > la {
		ar, br = br, ar
		la, lb = lb, la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = minInt(del, minInt(ins, sub))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
