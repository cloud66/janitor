package core

import (
	"errors"
	"fmt"
	"io"

	"golang.org/x/net/context"
)

// ctxKey is a package-private typed context key to prevent collisions with
// string-literal keys from other packages (go vet SA1029).
type ctxKey string

// Context keys used across the janitor codebase. Production callers populate
// these in main. Tests can override them to inject fakes or swap base URLs.
const (
	// credentials — one per cloud provider
	DOPatKey              ctxKey = "JANITOR_DO_PAT"
	AWSAccessKeyIDKey     ctxKey = "JANITOR_AWS_ACCESS_KEY_ID"
	AWSSecretAccessKeyKey ctxKey = "JANITOR_AWS_SECRET_ACCESS_KEY"
	VultrPatKey           ctxKey = "JANITOR_VULTR_PAT"
	HetznerPatKey         ctxKey = "JANITOR_HETZNER_PAT"

	// test-only: optional base-URL overrides per provider. When set, the
	// executor's client() method points the SDK at the given URL.
	DOBaseURLKey      ctxKey = "JANITOR_DO_BASE_URL"
	VultrBaseURLKey   ctxKey = "JANITOR_VULTR_BASE_URL"
	HetznerBaseURLKey ctxKey = "JANITOR_HETZNER_BASE_URL"

	// ExecutorKey is where main stashes the resolved ExecutorInterface for the
	// current cloud so helper funcs in main.go can pull it back out.
	ExecutorKey ctxKey = "executor"

	// WarnWriterKey optionally holds an io.Writer that executors use to surface
	// non-fatal warnings (e.g. unparseable Created timestamp). Populated by
	// main from the `out` sink; absent in tests → warnings are silently dropped.
	WarnWriterKey ctxKey = "janitor-warn-writer"
)

// ErrUnsupported is the sentinel returned by executors when a particular
// operation isn't supported by the underlying cloud provider. Callers compare
// via errors.Is instead of relying on the historical "action not available"
// string match.
var ErrUnsupported = errors.New("action not available")

// Warnf writes a warning message to the writer stored under WarnWriterKey on
// ctx. If no writer is set (common in tests), the warning is silently dropped.
// Each message is prefixed with "[WARN] " and terminated by a newline so that
// the output is greppable and easy to assert on in tests.
func Warnf(ctx context.Context, format string, args ...interface{}) {
	if ctx == nil {
		return
	}
	w, ok := ctx.Value(WarnWriterKey).(io.Writer)
	if !ok || w == nil {
		return
	}
	fmt.Fprintf(w, "[WARN] "+format+"\n", args...)
}
