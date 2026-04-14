package core

import (
	"sort"
	"testing"
)

// TestVolumeSorter_AgeDescending asserts age-desc ordering (oldest first).
func TestVolumeSorter_AgeDescending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc string
		in   []Volume
		want []float64
	}{
		{
			desc: "mixed ages sort desc",
			in:   []Volume{{Age: 1.0}, {Age: 5.0}, {Age: 2.0}},
			want: []float64{5.0, 2.0, 1.0},
		},
		{
			desc: "empty slice is a no-op",
			in:   []Volume{},
			want: []float64{},
		},
		{
			desc: "equal ages remain equal",
			in:   []Volume{{Age: 2.5, Name: "a"}, {Age: 2.5, Name: "b"}},
			want: []float64{2.5, 2.5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			s := append([]Volume(nil), tt.in...)
			sort.Sort(VolumeSorter(s))
			if len(s) != len(tt.want) {
				t.Fatalf("len got %d want %d", len(s), len(tt.want))
			}
			for i, v := range s {
				if v.Age != tt.want[i] {
					t.Errorf("pos %d: got Age=%v want %v", i, v.Age, tt.want[i])
				}
			}
		})
	}
}
