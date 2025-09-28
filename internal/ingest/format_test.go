package ingest

import (
	"errors"
	"testing"
)

func TestValidateArtifactFormats_AllSafe(t *testing.T) {
	files := []ManifestFile{
		{Path: "config.json"},
		{Path: "model.safetensors"},
		{Path: "tokenizer.json"},
	}
	if err := ValidateArtifactFormats(files); err != nil {
		t.Errorf("ValidateArtifactFormats() error = %v, want nil", err)
	}
}

func TestValidateArtifactFormats_RejectsEachUnsafeExtension(t *testing.T) {
	for _, ext := range []string{".bin", ".pt", ".pth", ".ckpt", ".BIN", ".Pt"} {
		files := []ManifestFile{
			{Path: "config.json"},
			{Path: "pytorch_model" + ext},
		}
		err := ValidateArtifactFormats(files)
		if !errors.Is(err, ErrUnsafeFormat) {
			t.Errorf("extension %q: error = %v, want ErrUnsafeFormat", ext, err)
		}
	}
}

func TestValidateArtifactFormats_RejectsMixedSafeAndUnsafe(t *testing.T) {
	files := []ManifestFile{
		{Path: "model.safetensors"},
		{Path: "pytorch_model.bin"},
	}
	if err := ValidateArtifactFormats(files); !errors.Is(err, ErrUnsafeFormat) {
		t.Errorf("error = %v, want ErrUnsafeFormat even with a safetensors file present", err)
	}
}

func TestValidateArtifactFormats_EmptyManifest(t *testing.T) {
	if err := ValidateArtifactFormats(nil); err != nil {
		t.Errorf("ValidateArtifactFormats(nil) error = %v, want nil", err)
	}
}
