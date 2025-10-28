// Package lora ingests and validates LoRA adapters, and routes and
// serves them against a shared resident base model.
package lora

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sanjayrohith/redline/internal/ingest"
)

// AdapterConfig mirrors the subset of a PEFT/LoRA adapter_config.json
// Redline validates before an adapter is served: which base model it was
// trained against, its rank, and which of the base model's modules it
// targets.
type AdapterConfig struct {
	BaseModelNameOrPath string   `json:"base_model_name_or_path"`
	R                   int      `json:"r"`
	TargetModules       []string `json:"target_modules"`
}

// ErrBaseModelMismatch means the adapter declares a different base model
// than the one it is being validated against.
var ErrBaseModelMismatch = errors.New("lora: adapter's declared base model does not match the resident base")

// ErrInvalidRank means the adapter's declared rank is not a positive
// integer.
var ErrInvalidRank = errors.New("lora: adapter rank must be positive")

// ErrUnsupportedTargetModule means the adapter targets a module the base
// model architecture does not expose.
var ErrUnsupportedTargetModule = errors.New("lora: adapter targets a module the base model does not have")

// ErrRankMismatch means the adapter's declared rank does not match the
// rank its own LoRA A/B tensor shapes actually have - a config.json that
// disagrees with the weights it ships alongside.
var ErrRankMismatch = errors.New("lora: adapter's declared rank does not match its tensor shapes")

// ValidateAdapter checks an adapter's declared config against the base
// model it is being attached to: the base model identifier must match
// exactly (a LoRA adapter's weights are only meaningful relative to the
// exact base it was trained against), rank must be positive, and every
// module the adapter targets must be one the base model architecture
// actually exposes.
func ValidateAdapter(adapter AdapterConfig, residentBaseModel string, baseSupportedModules []string) error {
	if adapter.BaseModelNameOrPath != residentBaseModel {
		return fmt.Errorf("%w: adapter declares %q, resident base is %q",
			ErrBaseModelMismatch, adapter.BaseModelNameOrPath, residentBaseModel)
	}

	if adapter.R <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidRank, adapter.R)
	}

	supported := make(map[string]bool, len(baseSupportedModules))
	for _, m := range baseSupportedModules {
		supported[m] = true
	}
	for _, target := range adapter.TargetModules {
		if !supported[target] {
			return fmt.Errorf("%w: %q", ErrUnsupportedTargetModule, target)
		}
	}

	return nil
}

// ValidateAdapterTensors cross-checks the adapter's declared rank against
// its actual LoRA A/B tensor shapes. PEFT's convention is
// lora_A.weight with shape [r, in_features] and lora_B.weight with shape
// [out_features, r]; a mismatch here means the adapter's config.json
// disagrees with the weights it ships alongside, which ValidateAdapter's
// config-only check cannot catch on its own.
func ValidateAdapterTensors(header *ingest.Header, declaredRank int) error {
	for _, t := range header.Tensors {
		switch {
		case strings.HasSuffix(t.Name, "lora_A.weight"):
			if len(t.Shape) != 2 || t.Shape[0] != int64(declaredRank) {
				return fmt.Errorf("%w: tensor %q has shape %v, want first dimension %d", ErrRankMismatch, t.Name, t.Shape, declaredRank)
			}
		case strings.HasSuffix(t.Name, "lora_B.weight"):
			if len(t.Shape) != 2 || t.Shape[1] != int64(declaredRank) {
				return fmt.Errorf("%w: tensor %q has shape %v, want second dimension %d", ErrRankMismatch, t.Name, t.Shape, declaredRank)
			}
		}
	}
	return nil
}
