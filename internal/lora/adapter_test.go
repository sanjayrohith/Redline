package lora_test

import (
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/ingest"
	"github.com/sanjayrohith/redline/internal/lora"
)

func validAdapter() lora.AdapterConfig {
	return lora.AdapterConfig{
		BaseModelNameOrPath: "meta-llama/Llama-2-7b-hf",
		R:                   16,
		TargetModules:       []string{"q_proj", "v_proj"},
	}
}

func TestValidateAdapter_Valid(t *testing.T) {
	err := lora.ValidateAdapter(validAdapter(), "meta-llama/Llama-2-7b-hf", []string{"q_proj", "k_proj", "v_proj", "o_proj"})
	if err != nil {
		t.Fatalf("ValidateAdapter() error = %v, want nil", err)
	}
}

func TestValidateAdapter_BaseModelMismatch(t *testing.T) {
	err := lora.ValidateAdapter(validAdapter(), "mistralai/Mistral-7B-v0.1", []string{"q_proj", "v_proj"})
	if !errors.Is(err, lora.ErrBaseModelMismatch) {
		t.Fatalf("ValidateAdapter() error = %v, want ErrBaseModelMismatch", err)
	}
}

func TestValidateAdapter_InvalidRank(t *testing.T) {
	adapter := validAdapter()
	adapter.R = 0
	err := lora.ValidateAdapter(adapter, adapter.BaseModelNameOrPath, []string{"q_proj", "v_proj"})
	if !errors.Is(err, lora.ErrInvalidRank) {
		t.Fatalf("ValidateAdapter() error = %v, want ErrInvalidRank", err)
	}
}

func TestValidateAdapter_UnsupportedTargetModule(t *testing.T) {
	adapter := validAdapter()
	adapter.TargetModules = []string{"q_proj", "gate_proj"}
	err := lora.ValidateAdapter(adapter, adapter.BaseModelNameOrPath, []string{"q_proj", "v_proj"})
	if !errors.Is(err, lora.ErrUnsupportedTargetModule) {
		t.Fatalf("ValidateAdapter() error = %v, want ErrUnsupportedTargetModule", err)
	}
}

func TestValidateAdapterTensors_Matching(t *testing.T) {
	header := &ingest.Header{Tensors: []ingest.TensorInfo{
		{Name: "base_model.model.layers.0.self_attn.q_proj.lora_A.weight", Shape: []int64{16, 4096}},
		{Name: "base_model.model.layers.0.self_attn.q_proj.lora_B.weight", Shape: []int64{4096, 16}},
	}}
	if err := lora.ValidateAdapterTensors(header, 16); err != nil {
		t.Fatalf("ValidateAdapterTensors() error = %v, want nil", err)
	}
}

func TestValidateAdapterTensors_RankMismatchInLoraA(t *testing.T) {
	header := &ingest.Header{Tensors: []ingest.TensorInfo{
		{Name: "base_model.model.layers.0.self_attn.q_proj.lora_A.weight", Shape: []int64{8, 4096}},
	}}
	err := lora.ValidateAdapterTensors(header, 16)
	if !errors.Is(err, lora.ErrRankMismatch) {
		t.Fatalf("ValidateAdapterTensors() error = %v, want ErrRankMismatch", err)
	}
}

func TestValidateAdapterTensors_RankMismatchInLoraB(t *testing.T) {
	header := &ingest.Header{Tensors: []ingest.TensorInfo{
		{Name: "base_model.model.layers.0.self_attn.q_proj.lora_B.weight", Shape: []int64{4096, 8}},
	}}
	err := lora.ValidateAdapterTensors(header, 16)
	if !errors.Is(err, lora.ErrRankMismatch) {
		t.Fatalf("ValidateAdapterTensors() error = %v, want ErrRankMismatch", err)
	}
}

func TestValidateAdapterTensors_IgnoresUnrelatedTensors(t *testing.T) {
	header := &ingest.Header{Tensors: []ingest.TensorInfo{
		{Name: "base_model.model.embed_tokens.weight", Shape: []int64{32000, 4096}},
	}}
	if err := lora.ValidateAdapterTensors(header, 16); err != nil {
		t.Fatalf("ValidateAdapterTensors() error = %v, want nil for a tensor that isn't a lora_A/B weight", err)
	}
}
