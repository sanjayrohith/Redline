package policy

import "math"

// ShannonEntropy returns s's Shannon entropy in bits per byte: how
// unpredictable its byte distribution is. Natural language prose sits
// well above 3.5; a scripted, repetitive, or near-empty prompt - the
// signature of a scraping or spam client cycling the same template -
// sits well below it.
func ShannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}

	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}

	total := float64(len(s))
	var entropy float64
	for _, c := range counts {
		if c == 0 {
			continue
		}
		p := float64(c) / total
		entropy -= p * math.Log2(p)
	}
	return entropy
}

// LowEntropyThreshold is the default cutoff below which a prompt is
// treated as suspiciously repetitive rather than natural language.
const LowEntropyThreshold = 2.5

// IsLowEntropy reports whether s's Shannon entropy falls at or below
// threshold.
func IsLowEntropy(s string, threshold float64) bool {
	return ShannonEntropy(s) <= threshold
}
