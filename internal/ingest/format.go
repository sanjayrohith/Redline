package ingest

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrUnsafeFormat marks a repository containing at least one
// pickle-backed artifact. Deserializing such a file executes arbitrary
// Python, so ingestion refuses it outright rather than attempting to load
// only the "safe-looking" files alongside it.
var ErrUnsafeFormat = errors.New("ingest: UNSAFE_FORMAT")

var unsafeExtensions = map[string]bool{
	".bin":  true,
	".pt":   true,
	".pth":  true,
	".ckpt": true,
}

// ValidateArtifactFormats rejects the manifest if it contains any
// pickle-backed artifact (.bin, .pt, .pth, .ckpt), regardless of whether
// safetensors files are also present.
func ValidateArtifactFormats(files []ManifestFile) error {
	var unsafe []string
	for _, f := range files {
		if unsafeExtensions[strings.ToLower(filepath.Ext(f.Path))] {
			unsafe = append(unsafe, f.Path)
		}
	}

	if len(unsafe) > 0 {
		return fmt.Errorf("%w: pickle-backed artifact(s) execute arbitrary code on load: %s",
			ErrUnsafeFormat, strings.Join(unsafe, ", "))
	}

	return nil
}
