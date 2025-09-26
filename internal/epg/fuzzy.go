package epg

// FindBest sucht in nameToID nach dem Eintrag, der am nächsten an name (unscharf) dran ist.
// maxDist gibt die maximale zulässige Editierdistanz an.
func FindBest(name string, nameToID map[string]string, maxDist int) (string, bool) {
	key := NameKey(name)
	
	// Exakter Treffer
	if id, ok := nameToID[key]; ok {
		return id, true
	}

	// Fuzzy-Suche
	bestID := ""
	bestDist := maxDist + 1

	for k, id := range nameToID {
		dist := levenshtein(key, k)
		if dist < bestDist {
			bestDist = dist
			bestID = id
		}
	}

	if bestDist <= maxDist {
		return bestID, true
	}
	return "", false
}

// Levenshtein-Distanz Berechnung
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	lenA, lenB := len(ra), len(rb)

	// Edge cases
	if lenA == 0 {
		return lenB
	}
	if lenB == 0 {
		return lenA
	}

	// DP Matrix
	dp := make([][]int, lenA+1)
	for i := range dp {
		dp[i] = make([]int, lenB+1)
		dp[i][0] = i
	}
	for j := 0; j <= lenB; j++ {
		dp[0][j] = j
	}

	// Berechnung
	for i := 1; i <= lenA; i++ {
		for j := 1; j <= lenB; j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			dp[i][j] = min(
				dp[i-1][j]+1,    // deletion
				dp[i][j-1]+1,    // insertion
				dp[i-1][j-1]+cost, // substitution
			)
		}
	}
	return dp[lenA][lenB]
}

func min(x, y, z int) int {
	if x < y {
		if x < z {
			return x
		}
		return z
	}
	if y < z {
		return y
	}
	return z
}
