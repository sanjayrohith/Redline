package ingest

import "testing"

func TestParseReference_Valid(t *testing.T) {
	tests := []struct {
		input        string
		wantOwner    string
		wantName     string
		wantRevision string
	}{
		{"meta-llama/Llama-3-8B", "meta-llama", "Llama-3-8B", "main"},
		{"https://huggingface.co/meta-llama/Llama-3-8B", "meta-llama", "Llama-3-8B", "main"},
		{"https://huggingface.co/meta-llama/Llama-3-8B/tree/main", "meta-llama", "Llama-3-8B", "main"},
		{"https://huggingface.co/meta-llama/Llama-3-8B/tree/v2.1", "meta-llama", "Llama-3-8B", "v2.1"},
		{"  meta-llama/Llama-3-8B  ", "meta-llama", "Llama-3-8B", "main"},
		{"org.with.dots/model_with_underscores", "org.with.dots", "model_with_underscores", "main"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ref, err := ParseReference(tt.input)
			if err != nil {
				t.Fatalf("ParseReference(%q) error = %v", tt.input, err)
			}
			if ref.Owner != tt.wantOwner || ref.Name != tt.wantName || ref.Revision != tt.wantRevision {
				t.Errorf("got %+v, want owner=%q name=%q revision=%q", ref, tt.wantOwner, tt.wantName, tt.wantRevision)
			}
		})
	}
}

func TestParseReference_Invalid(t *testing.T) {
	tests := []string{
		"",
		"   ",
		"just-one-segment",
		"too/many/segments/here",
		"/leading-slash-empty-owner",
		"trailing-slash-empty-name/",
		"org/model with spaces",
		"https://github.com/meta-llama/Llama-3-8B",
		"https://evil.co/meta-llama/Llama-3-8B",
		"ftp://huggingface.co/meta-llama/Llama-3-8B",
		"https://huggingface.co/",
		"https://huggingface.co/only-owner",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseReference(input); err == nil {
				t.Errorf("ParseReference(%q) error = nil, want error", input)
			}
		})
	}
}

func TestReference_RepoID(t *testing.T) {
	ref := Reference{Owner: "meta-llama", Name: "Llama-3-8B"}
	if got := ref.RepoID(); got != "meta-llama/Llama-3-8B" {
		t.Errorf("RepoID() = %q, want meta-llama/Llama-3-8B", got)
	}
}
