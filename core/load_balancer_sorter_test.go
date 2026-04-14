package core

import (
	"sort"
	"testing"
)

// TestLoadBalancerSorter_AgeDescending asserts age-desc ordering (oldest first).
// B1: the type's former doc said "by name" — actual behavior is age-desc.
func TestLoadBalancerSorter_AgeDescending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc string
		in   []LoadBalancer
		want []float64
	}{
		{
			desc: "mixed ages sort desc",
			in:   []LoadBalancer{{Age: 1.0}, {Age: 5.0}, {Age: 2.0}},
			want: []float64{5.0, 2.0, 1.0},
		},
		{
			desc: "empty slice is a no-op",
			in:   []LoadBalancer{},
			want: []float64{},
		},
		// equal ages — preserve any stable relative ordering of ages (values equal)
		{
			desc: "equal ages remain equal",
			in:   []LoadBalancer{{Age: 3.0, Name: "a"}, {Age: 3.0, Name: "b"}, {Age: 3.0, Name: "c"}},
			want: []float64{3.0, 3.0, 3.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			// sort a copy so subtests don't share state
			s := append([]LoadBalancer(nil), tt.in...)
			sort.Sort(LoadBalancerSorter(s))
			if len(s) != len(tt.want) {
				t.Fatalf("len got %d want %d", len(s), len(tt.want))
			}
			for i, lb := range s {
				if lb.Age != tt.want[i] {
					t.Errorf("pos %d: got Age=%v want %v", i, lb.Age, tt.want[i])
				}
			}
		})
	}
}
