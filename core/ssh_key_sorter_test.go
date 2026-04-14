package core

import (
	"sort"
	"testing"
)

// TestSshKeySorter_Ascending asserts ascending VendorID (string) sort.
func TestSshKeySorter_Ascending(t *testing.T) {
	t.Parallel()

	t.Run("alphabetic ascending", func(t *testing.T) {
		t.Parallel()
		in := []SshKey{{VendorID: "c"}, {VendorID: "a"}, {VendorID: "b"}}
		sort.Sort(SshKeySorter(in))
		want := []string{"a", "b", "c"}
		for i, k := range in {
			if k.VendorID != want[i] {
				t.Errorf("pos %d: got %q want %q", i, k.VendorID, want[i])
			}
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		t.Parallel()
		in := []SshKey{}
		sort.Sort(SshKeySorter(in))
		if len(in) != 0 {
			t.Errorf("empty slice grew to %d", len(in))
		}
	})

	// B2: SshKeySorter uses string `<` on VendorID, so "v10" sorts before "v2".
	// This pins current behavior — deleteSshKeys keep-N may drop newer keys because
	// lexicographic order diverges from the intended numeric/creation order.
	t.Run("lexicographic_gotcha_B2", func(t *testing.T) {
		t.Parallel()
		in := []SshKey{{VendorID: "v1"}, {VendorID: "v2"}, {VendorID: "v10"}}
		sort.Sort(SshKeySorter(in))
		want := []string{"v1", "v10", "v2"}
		for i, k := range in {
			if k.VendorID != want[i] {
				t.Errorf("B2 pin: pos %d got %q want %q", i, k.VendorID, want[i])
			}
		}
	})
}
