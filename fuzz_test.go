package main

import (
	"strings"
	"testing"
)

// referenceHasSampleTag is an INDEPENDENT reference implementation that
// encodes the INTENT of hasSampleTag rather than mirroring its code. It uses
// different primitives (strings.Cut, strings.EqualFold, strings.Contains on
// the lowered value only) so the fuzz oracle catches real divergences, not
// textually-identical behaviour.
//
// Intent: for each tag, split on the FIRST "=" into key/value; if the
// trimmed key equals "C66-STACK" case-insensitively, and the value
// (lowercased) contains "sample", return true.
func referenceHasSampleTag(tags []string) bool {
	for _, tag := range tags {
		// split on the first "=" using strings.Cut — returns key, value, found
		key, value, found := strings.Cut(tag, "=")
		if !found {
			// no "=" → not a key=value tag
			continue
		}
		// case-insensitive key compare after trimming whitespace
		if !strings.EqualFold(strings.TrimSpace(key), "C66-STACK") {
			continue
		}
		// scan ONLY the value portion, case-insensitively
		if strings.Contains(strings.ToLower(value), "sample") {
			return true
		}
	}
	return false
}

// TestHasSampleTag_B3_Reproducer documents the specific input the fuzzer
// discovered that exposed B3, now fixed. Acts as a regression test.
func TestHasSampleTag_B3_Reproducer(t *testing.T) {
	const reproducer = "C66-STACK =SAMPLE"
	got := hasSampleTag([]string{reproducer})
	want := referenceHasSampleTag([]string{reproducer})
	if got != want {
		t.Fatalf("discrepancy for %q: hasSampleTag=%v reference=%v", reproducer, got, want)
	}
	// positive assertion: this specific input must now match post-fix
	if !got {
		t.Fatalf("expected hasSampleTag(%q) = true after B3 fix, got false", reproducer)
	}
}

// TestHasSampleTag_Multiple_Equals pins the intentional first-"=" split
// semantics. The value portion may itself contain "=" characters; we must
// scan the whole value for "sample", not a re-split portion.
func TestHasSampleTag_Multiple_Equals(t *testing.T) {
	// pure-function — safe to parallelise
	t.Parallel()
	tests := []struct {
		desc     string
		tag      string
		expected bool
	}{
		// value after first "=" is "a=sample" → contains "sample"
		{"value contains embedded equals and sample", "C66-STACK=a=sample", true},
		// value after first "=" is "a=b" → no "sample"
		{"value contains embedded equals without sample", "C66-STACK=a=b", false},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			got := hasSampleTag([]string{tt.tag})
			if got != tt.expected {
				t.Errorf("hasSampleTag(%q) = %v, want %v", tt.tag, got, tt.expected)
			}
		})
	}
}

// FuzzHasSampleTag checks the production hasSampleTag against an independent
// reference. Divergences indicate real bugs, not textual drift.
func FuzzHasSampleTag(f *testing.F) {
	// seed corpus — known-interesting shapes and historical bug inputs
	seeds := []string{
		"C66-STACK=maestro-sample-prd",
		"c66-stack=nope",
		"xc66-stack=sample",
		"C66-STACK=SAMPLE",
		"C66-STACK =SAMPLE",
		"C66-STACK=a=sample",
		"samplekey=x",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, tag string) {
		// split on NUL to get a multi-tag input — exercises the per-tag OR
		// loop, not just single-tag short-circuit. addresses panel C10.
		tags := strings.Split(tag, "\x00")
		got := hasSampleTag(tags)
		want := referenceHasSampleTag(tags)
		if got != want {
			t.Fatalf("discrepancy for %q: hasSampleTag=%v reference=%v", tag, got, want)
		}
	})
}
