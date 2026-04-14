package main

import (
	"strings"
	"testing"
)

// referenceHasSampleTag is the strict reference implementation we believe is
// correct: split each tag on the FIRST "=", compare the key (trimmed, case
// insensitive) to "c66-stack", and scan ONLY the value portion for "sample".
// The production hasSampleTag uses HasPrefix + Contains on the whole string,
// which can false-positive (B3).
func referenceHasSampleTag(tags []string) bool {
	for _, tag := range tags {
		// split key=value on the first "="
		i := strings.IndexByte(tag, '=')
		if i < 0 {
			// no "=" → no value to inspect
			continue
		}
		key := strings.TrimSpace(strings.ToLower(tag[:i]))
		val := strings.ToLower(tag[i+1:])
		// only the C66-STACK key qualifies
		if key != "c66-stack" {
			continue
		}
		// scan only the value portion for "sample"
		if strings.Contains(val, "sample") {
			return true
		}
	}
	return false
}

// TestHasSampleTag_B3_Reproducer documents the specific input the fuzzer
// discovered that exposes B3. Left skipped so CI stays green until B3 is
// fixed in Phase 6 (P6-T1). When B3 is fixed, remove the t.Skip and this
// becomes a regression test for the fix.
//
// Discrepancy: for tag "C66-STACK =SAMPLE" (trailing space on key, before `=`)
// the production hasSampleTag returns false because HasPrefix(lower,
// "c66-stack=") fails on "c66-stack ="; the strict reference returns true
// because it trims the key before comparing and the value is "SAMPLE".
//
// This input is intentionally NOT committed under testdata/fuzz/ — a
// committed fuzz corpus replays on every `go test` and would fail CI until
// Phase 6 lands. Document-in-code instead.
// After the P6-T1 fix, the production hasSampleTag must agree with the strict
// reference for the original reproducer input — the key trims to "c66-stack"
// and the value "SAMPLE" contains "sample", so both return true.
func TestHasSampleTag_B3_Reproducer(t *testing.T) {
	const reproducer = "C66-STACK =SAMPLE"
	got := hasSampleTag([]string{reproducer})
	want := referenceHasSampleTag([]string{reproducer})
	if got != want {
		t.Fatalf("discrepancy for %q: hasSampleTag=%v reference=%v", reproducer, got, want)
	}
	// positive assertion: this specific input should now match
	if !got {
		t.Fatalf("expected hasSampleTag(%q) = true after B3 fix, got false", reproducer)
	}
}

// FuzzHasSampleTag looks for inputs where the production hasSampleTag
// disagrees with the strict reference. Any discrepancy confirms B3.
func FuzzHasSampleTag(f *testing.F) {
	// seed corpus from the plan — these are the known-interesting shapes
	seeds := []string{
		"C66-STACK=maestro-sample-prd",
		"c66-stack=nope",
		"xc66-stack=sample",
		"C66-STACK=SAMPLE",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, tag string) {
		// single-tag fuzzing is sufficient — the function is a per-tag OR
		tags := []string{tag}
		got := hasSampleTag(tags)
		want := referenceHasSampleTag(tags)
		if got != want {
			t.Fatalf("discrepancy for %q: hasSampleTag=%v reference=%v", tag, got, want)
		}
	})
}
