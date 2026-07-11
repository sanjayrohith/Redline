package policy_test

import (
	"strings"
	"testing"

	"github.com/sanjayrohith/redline/internal/policy"
)

func TestShannonEntropy_RepeatedCharacterIsZero(t *testing.T) {
	if got := policy.ShannonEntropy("aaaaaaaaaa"); got != 0 {
		t.Errorf("ShannonEntropy(repeated char) = %f, want 0", got)
	}
}

func TestShannonEntropy_EmptyStringIsZero(t *testing.T) {
	if got := policy.ShannonEntropy(""); got != 0 {
		t.Errorf("ShannonEntropy(\"\") = %f, want 0", got)
	}
}

func TestShannonEntropy_NaturalLanguageScoresHigherThanRepetition(t *testing.T) {
	prose := "The quick brown fox jumps over the lazy dog near the riverbank at dawn."
	repetitive := strings.Repeat("aaaa bbbb ", 8)

	proseEntropy := policy.ShannonEntropy(prose)
	repetitiveEntropy := policy.ShannonEntropy(repetitive)

	if proseEntropy <= repetitiveEntropy {
		t.Errorf("prose entropy %f should exceed repetitive entropy %f", proseEntropy, repetitiveEntropy)
	}
}

func TestIsLowEntropy_ThresholdBehavior(t *testing.T) {
	if !policy.IsLowEntropy("aaaaaaaaaa", policy.LowEntropyThreshold) {
		t.Error("IsLowEntropy(repeated char) = false, want true")
	}
	if policy.IsLowEntropy("The quick brown fox jumps over the lazy dog", policy.LowEntropyThreshold) {
		t.Error("IsLowEntropy(natural language) = true, want false")
	}
}
