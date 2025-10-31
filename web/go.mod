// This file exists only to mark web/ as a separate Go module boundary, so
// `go build|vet|test ./...` run from the repo root does not descend into
// web/node_modules (some npm packages, e.g. flatted, ship stray .go files).
// The web/ directory has no Go code of its own.
module github.com/sanjayrohith/redline/web-boundary

go 1.27.0
