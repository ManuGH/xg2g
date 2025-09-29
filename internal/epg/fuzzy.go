// SPDX-License-Identifier: MIT
package epg

// FindBest: exaktes oder fuzzy Matching (Levenshtein) bis maxDist
func FindBest(name string, nameToID map[string]string, maxDist int) (string, bool) {
	key := NameKey(name)

	if id, ok := nameToID[key]; ok {
		return id, true
	}

	bestID := ""
	best := maxDist + 1
	for k, id := range nameToID {
		if d := levenshtein(key, k); d < best {
			best, bestID = d, id
		}
	}
	if best <= maxDist {
		return bestID, true
	}
	return "", false
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	dp := make([]int, (la+1)*(lb+1))
	idx := func(i, j int) int { return i*(lb+1) + j }
	for i := 0; i <= la; i++ {
		dp[idx(i, 0)] = i
	}
	for j := 0; j <= lb; j++ {
		dp[idx(0, j)] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			del := dp[idx(i-1, j)] + 1
			ins := dp[idx(i, j-1)] + 1
			sub := dp[idx(i-1, j-1)] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			dp[idx(i, j)] = m
		}
	}
	return dp[idx(la, lb)]
}
