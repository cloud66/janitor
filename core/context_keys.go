package core

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// ctxKey is a package-private typed context key to prevent collisions with
// string-literal keys from other packages (go vet SA1029).
type ctxKey string

// context keys used across the janitor codebase. production callers populate
// these in main. tests can override them to inject fakes or swap base URLs.
const (
	// credentials — one per cloud provider
	DOPatKey              ctxKey = "JANITOR_DO_PAT"
	AWSAccessKeyIDKey     ctxKey = "JANITOR_AWS_ACCESS_KEY_ID"
	AWSSecretAccessKeyKey ctxKey = "JANITOR_AWS_SECRET_ACCESS_KEY"
	VultrPatKey           ctxKey = "JANITOR_VULTR_PAT"
	HetznerPatKey         ctxKey = "JANITOR_HETZNER_PAT"

	// test-only: optional base-URL overrides per provider. when set, the
	// executor's client() method points the SDK at the given URL.
	DOBaseURLKey      ctxKey = "JANITOR_DO_BASE_URL"
	VultrBaseURLKey   ctxKey = "JANITOR_VULTR_BASE_URL"
	HetznerBaseURLKey ctxKey = "JANITOR_HETZNER_BASE_URL"

	// ExecutorKey is where main stashes the resolved ExecutorInterface for the
	// current cloud so helper funcs in main.go can pull it back out.
	ExecutorKey ctxKey = "executor"

	// WarnWriterKey optionally holds an io.Writer that executors use to surface
	// non-fatal warnings (e.g. unparseable Created timestamp). populated by
	// main from the `out` sink; absent in tests → warnings are silently dropped.
	WarnWriterKey ctxKey = "janitor-warn-writer"

	// OutWriterKey holds the executor's normal (non-warning) output writer —
	// used by mock markers and progress lines. populated by main from the same
	// `out` sink; tests may set it to a buffer to assert on printed output.
	OutWriterKey ctxKey = "janitor-out-writer"
)

// TagKeyC66Stack is the canonical cloud66 stack tag key. matched
// case-insensitively so "C66-STACK" and "c66-stack" both hit.
const TagKeyC66Stack = "C66-STACK"

// tag-value markers used by the janitor classification logic. compared via
// case-insensitive substring match against resource names and tag values.
const (
	TagPermanent = "permanent"
	TagLong      = "long"
	TagSample    = "sample"
)

// ErrUnsupported is the sentinel returned by executors when a particular
// operation isn't supported by the underlying cloud provider. callers compare
// via errors.Is instead of relying on the historical "action not available"
// string match.
var ErrUnsupported = errors.New("action not available")

// Warnf writes a warning message to the writer stored under WarnWriterKey on
// ctx. if no writer is set (common in tests), the warning is silently dropped.
// each message is prefixed with "[WARN] " and terminated by a newline so that
// the output is greppable and easy to assert on in tests.
func Warnf(ctx context.Context, format string, args ...interface{}) {
	if ctx == nil {
		return
	}
	w, ok := ctx.Value(WarnWriterKey).(io.Writer)
	if !ok || w == nil {
		return
	}
	// intentionally ignore the Fprintf error — the warning writer is a
	// best-effort sink (stdout / bytes.Buffer in tests) where write failures
	// carry no recoverable information.
	_, _ = fmt.Fprintf(w, "[WARN] "+format+"\n", args...)
}

// Writef writes a normal (non-warning) message to the writer stored under
// OutWriterKey on ctx. if no writer is set the message is silently dropped.
// unlike Warnf, no prefix or trailing newline is added — callers format as
// they wish.
func Writef(ctx context.Context, format string, args ...interface{}) {
	if ctx == nil {
		return
	}
	w, ok := ctx.Value(OutWriterKey).(io.Writer)
	if !ok || w == nil {
		return
	}
	// best-effort write — see Warnf comment for rationale.
	_, _ = fmt.Fprintf(w, format, args...)
}
