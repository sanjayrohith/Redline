package ingest

import "testing"

func llamaLikeHeader() *Header {
	return &Header{Tensors: []TensorInfo{
		{Name: "model.embed_tokens.weight", Shape: []int64{32000, 4096}},
		{Name: "model.layers.0.self_attn.q_proj.weight", Shape: []int64{4096, 4096}},
		{Name: "model.layers.0.mlp.gate_proj.weight", Shape: []int64{11008, 4096}},
		{Name: "lm_head.weight", Shape: []int64{32000, 4096}},
	}}
}

func TestParameterCount(t *testing.T) {
	header := &Header{Tensors: []TensorInfo{
		{Shape: []int64{2, 3}},    // 6
		{Shape: []int64{10}},      // 10
		{Shape: []int64{4, 4, 4}}, // 64
	}}

	if got := ParameterCount(header); got != 80 {
		t.Errorf("ParameterCount() = %d, want 80", got)
	}
}

func TestParameterCount_ScalarTensor(t *testing.T) {
	header := &Header{Tensors: []TensorInfo{{Shape: []int64{}}}}
	if got := ParameterCount(header); got != 1 {
		t.Errorf("ParameterCount() = %d, want 1 for a scalar (rank-0) tensor", got)
	}
}

func TestInferArchitecture_FromConfigArchitectures(t *testing.T) {
	cfg := &ModelConfig{Architectures: []string{"LlamaForCausalLM"}}
	if got := InferArchitecture(cfg, &Header{}); got != "llama" {
		t.Errorf("InferArchitecture() = %q, want llama", got)
	}
}

func TestInferArchitecture_FromConfigModelType(t *testing.T) {
	cfg := &ModelConfig{ModelType: "mistral"}
	if got := InferArchitecture(cfg, &Header{}); got != "mistral" {
		t.Errorf("InferArchitecture() = %q, want mistral", got)
	}
}

func TestInferArchitecture_PrefersArchitecturesOverModelType(t *testing.T) {
	cfg := &ModelConfig{ModelType: "text", Architectures: []string{"GPT2LMHeadModel"}}
	if got := InferArchitecture(cfg, &Header{}); got != "gpt2" {
		t.Errorf("InferArchitecture() = %q, want gpt2", got)
	}
}

func TestInferArchitecture_FallsBackToTensorNames(t *testing.T) {
	if got := InferArchitecture(nil, llamaLikeHeader()); got != "llama" {
		t.Errorf("InferArchitecture() = %q, want llama", got)
	}
}

func TestInferArchitecture_NoConfigNoRecognizableTensors(t *testing.T) {
	header := &Header{Tensors: []TensorInfo{{Name: "some.custom.weight"}}}
	if got := InferArchitecture(nil, header); got != "unknown" {
		t.Errorf("InferArchitecture() = %q, want unknown", got)
	}
}

func TestInferArchitecture_EmptyConfigFallsBackToTensorNames(t *testing.T) {
	cfg := &ModelConfig{}
	if got := InferArchitecture(cfg, llamaLikeHeader()); got != "llama" {
		t.Errorf("InferArchitecture() = %q, want llama", got)
	}
}

func TestNormalizeArchitectureClassName(t *testing.T) {
	tests := map[string]string{
		"LlamaForCausalLM":   "llama",
		"MistralForCausalLM": "mistral",
		"GPT2LMHeadModel":    "gpt2",
		"BertModel":          "bert",
	}
	for input, want := range tests {
		if got := normalizeArchitectureClassName(input); got != want {
			t.Errorf("normalizeArchitectureClassName(%q) = %q, want %q", input, got, want)
		}
	}
}
