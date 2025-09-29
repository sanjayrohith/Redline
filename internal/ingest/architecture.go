package ingest

import "strings"

// ParameterCount sums the element count of every tensor's shape - the
// model's total parameter count, computed purely from header metadata
// with no tensor payload ever read.
func ParameterCount(header *Header) int64 {
	var total int64
	for _, t := range header.Tensors {
		total += shapeElementCount(t.Shape)
	}
	return total
}

func shapeElementCount(shape []int64) int64 {
	n := int64(1)
	for _, dim := range shape {
		n *= dim
	}
	return n
}

// ModelConfig is the subset of a repository's config.json this gateway
// uses to identify architecture family.
type ModelConfig struct {
	ModelType     string   `json:"model_type"`
	Architectures []string `json:"architectures"`
}

// tensorNamingSignatures maps a distinguishing tensor name prefix pair to
// the architecture family it identifies, used as a fallback when
// config.json is absent or uninformative.
var tensorNamingSignatures = []struct {
	family        string
	requiredAny   []string
	requiredExtra string
}{
	{family: "llama", requiredAny: []string{"model.layers."}, requiredExtra: "model.embed_tokens."},
	{family: "gpt2", requiredAny: []string{"transformer.h."}, requiredExtra: "transformer.wte."},
	{family: "gpt-neox", requiredAny: []string{"gpt_neox.layers."}, requiredExtra: "gpt_neox.embed_in."},
	{family: "bert", requiredAny: []string{"bert.encoder.layer."}, requiredExtra: "bert.embeddings."},
	{family: "falcon", requiredAny: []string{"transformer.h."}, requiredExtra: "transformer.word_embeddings."},
}

// InferArchitecture identifies the model's architecture family, preferring
// config.json's declared architecture class or model_type and falling
// back to the tensor naming convention when config is nil or uninformative.
func InferArchitecture(config *ModelConfig, header *Header) string {
	if config != nil {
		if len(config.Architectures) > 0 && config.Architectures[0] != "" {
			return normalizeArchitectureClassName(config.Architectures[0])
		}
		if config.ModelType != "" {
			return strings.ToLower(config.ModelType)
		}
	}

	return inferArchitectureFromTensorNames(header)
}

func inferArchitectureFromTensorNames(header *Header) string {
	for _, sig := range tensorNamingSignatures {
		if hasAnyPrefix(header, sig.requiredAny) && hasAnyPrefix(header, []string{sig.requiredExtra}) {
			return sig.family
		}
	}
	return "unknown"
}

func hasAnyPrefix(header *Header, prefixes []string) bool {
	for _, t := range header.Tensors {
		for _, p := range prefixes {
			if strings.HasPrefix(t.Name, p) {
				return true
			}
		}
	}
	return false
}

// normalizeArchitectureClassName reduces a Transformers model class name
// like "LlamaForCausalLM" or "GPT2LMHeadModel" to a short lowercase family
// identifier like "llama" or "gpt2".
func normalizeArchitectureClassName(className string) string {
	name := strings.ToLower(className)
	for _, suffix := range []string{"forcausallm", "lmheadmodel", "model"} {
		if trimmed, ok := strings.CutSuffix(name, suffix); ok {
			return trimmed
		}
	}
	return name
}
