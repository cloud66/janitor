package core

import (
	"sort"
	"testing"
)

// TestServerSorter_AgeDescending asserts age-desc ordering (oldest first).
func TestServerSorter_AgeDescending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc string
		in   []Server
		want []float64
	}{
		{
			desc: "mixed ages sort desc",
			in:   []Server{{Age: 1.0}, {Age: 5.0}, {Age: 2.0}},
			want: []float64{5.0, 2.0, 1.0},
		},
		{
			desc: "empty slice is a no-op",
			in:   []Server{},
			want: []float64{},
		},
		{
			desc: "equal ages remain equal",
			in:   []Server{{Age: 4.0, Name: "a"}, {Age: 4.0, Name: "b"}},
			want: []float64{4.0, 4.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			s := append([]Server(nil), tt.in...)
			sort.Sort(ServerSorter(s))
			if len(s) != len(tt.want) {
				t.Fatalf("len got %d want %d", len(s), len(tt.want))
			}
			for i, srv := range s {
				if srv.Age != tt.want[i] {
					t.Errorf("pos %d: got Age=%v want %v", i, srv.Age, tt.want[i])
				}
			}
		})
	}
}
