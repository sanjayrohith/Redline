// Package ingest parses, fetches, and validates model artifacts from
// upstream model hosts before any weight bytes are downloaded.
package ingest

import (
	"fmt"
	"net/url"
	"strings"
)

// Reference is a canonical, host-qualified pointer to one model repository
// at one revision.
type Reference struct {
	Owner    string
	Name     string
	Revision string
}

// RepoID is the "owner/name" identifier the upstream API expects.
func (r Reference) RepoID() string {
	return r.Owner + "/" + r.Name
}

const defaultRevision = "main"

var supportedHosts = map[string]bool{
	"huggingface.co":     true,
	"www.huggingface.co": true,
}

// ParseReference normalizes a pasted repository URL or a bare "org/model"
// identifier into a canonical Reference, rejecting malformed input and any
// host other than huggingface.co before a single network call is made.
func ParseReference(input string) (*Reference, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("ingest: repository reference must not be empty")
	}

	if strings.Contains(input, "://") {
		return parseReferenceURL(input)
	}
	return parseBareReference(input)
}

func parseBareReference(input string) (*Reference, error) {
	owner, name, ok := strings.Cut(input, "/")
	if !ok || !validSegment(owner) || !validSegment(name) {
		return nil, fmt.Errorf("ingest: %q is not a valid org/model reference", input)
	}
	return &Reference{Owner: owner, Name: name, Revision: defaultRevision}, nil
}

func parseReferenceURL(input string) (*Reference, error) {
	u, err := url.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("ingest: malformed url %q: %w", input, err)
	}

	if u.Scheme != "https" {
		return nil, fmt.Errorf("ingest: unsupported url scheme %q, only https is supported", u.Scheme)
	}
	if !supportedHosts[u.Host] {
		return nil, fmt.Errorf("ingest: unsupported host %q, only huggingface.co is supported", u.Host)
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) < 2 || !validSegment(segments[0]) || !validSegment(segments[1]) {
		return nil, fmt.Errorf("ingest: url path %q does not contain a valid org/model reference", u.Path)
	}

	revision := defaultRevision
	if len(segments) >= 4 && segments[2] == "tree" && validSegment(segments[3]) {
		revision = segments[3]
	}

	return &Reference{Owner: segments[0], Name: segments[1], Revision: revision}, nil
}

func validSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}
