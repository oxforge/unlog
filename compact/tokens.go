package compact

// EstimateTokens returns a rough token count for s using the heuristic
// len(s) / 3.5, rounded up. This avoids CGO tiktoken while staying within
// ±15% of real tokenizer counts for typical log text.
func EstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	// Multiply by 2 then divide by 7 to avoid floating-point; equivalent to / 3.5.
	// +6 is (7-1) for ceiling division.
	return (len(s)*2 + 6) / 7
}
