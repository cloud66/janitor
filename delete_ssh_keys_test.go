package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cloud66/janitor/core"
)

// fakeExecutor is a minimal core.ExecutorInterface impl used by main-package
// tests. Records *Delete calls so tests can assert on invocation counts —
// the reviewer panel flagged stdout-only assertions as too weak (a broken
// skip that still printed the tag would pass).
type fakeExecutor struct {
	deletedKeys    []core.SshKey
	deletedLBs     []core.LoadBalancer
	deletedVolumes []core.Volume
}

func (f *fakeExecutor) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	return nil, core.ErrUnsupported
}
func (f *fakeExecutor) ServerDelete(ctx context.Context, s core.Server) error {
	return core.ErrUnsupported
}
func (f *fakeExecutor) ServerStop(ctx context.Context, s core.Server) error {
	return core.ErrUnsupported
}
func (f *fakeExecutor) ServerStart(ctx context.Context, s core.Server) error {
	return core.ErrUnsupported
}
func (f *fakeExecutor) LoadBalancersGet(ctx context.Context, mock bool) ([]core.LoadBalancer, error) {
	return nil, core.ErrUnsupported
}
func (f *fakeExecutor) LoadBalancerDelete(ctx context.Context, lb core.LoadBalancer) error {
	f.deletedLBs = append(f.deletedLBs, lb)
	return nil
}
func (f *fakeExecutor) SshKeysGet(ctx context.Context) ([]core.SshKey, error) {
	return nil, core.ErrUnsupported
}
func (f *fakeExecutor) SshKeyDelete(ctx context.Context, k core.SshKey) error {
	f.deletedKeys = append(f.deletedKeys, k)
	return nil
}
func (f *fakeExecutor) VolumesGet(ctx context.Context) ([]core.Volume, error) {
	return nil, core.ErrUnsupported
}
func (f *fakeExecutor) VolumeDelete(ctx context.Context, v core.Volume) error {
	f.deletedVolumes = append(f.deletedVolumes, v)
	return nil
}

// withSshKeepCount swaps flagSshKeysKeepCount for the test and restores via
// t.Cleanup so boundary tests don't leak state across -shuffle=on runs.
func withSshKeepCount(t *testing.T, keep int) {
	t.Helper()
	prev := flagSshKeysKeepCount
	flagSshKeysKeepCount = keep
	t.Cleanup(func() { flagSshKeysKeepCount = prev })
}

// TestDeleteSshKeys_KeepBoundaries exercises P2-T7 and P3-T6:
// assert deletion count for N=keep, N=keep+1, N=0 and assert the "keep"
// skip-reason string is printed for retained keys (P3-T6).
func TestDeleteSshKeys_KeepBoundaries(t *testing.T) {
	// deleteSshKeys uses flagMock=true path so we still exercise output
	// without hitting the fake's Delete; we separately force mock=false for
	// deletion-count assertions below.
	tests := []struct {
		desc          string
		keep          int
		keys          []core.SshKey
		wantDeletions int
		wantOutputs   []string
	}{
		{
			desc: "N equals keep — zero deletions",
			keep: 3,
			keys: []core.SshKey{
				{VendorID: "1", Name: "c66-a"},
				{VendorID: "2", Name: "c66-b"},
				{VendorID: "3", Name: "c66-c"},
			},
			wantDeletions: 0,
			wantOutputs:   []string{"skipped (keep last 3)"},
		},
		{
			desc: "N is keep+1 — exactly one deletion",
			keep: 2,
			keys: []core.SshKey{
				{VendorID: "1", Name: "c66-a"},
				{VendorID: "2", Name: "c66-b"},
				{VendorID: "3", Name: "c66-c"},
			},
			wantDeletions: 1,
			wantOutputs:   []string{"Deleted!", "skipped (keep last 2)"},
		},
		{
			desc: "N=0 — no c66 keys to delete",
			keep: 2,
			keys: []core.SshKey{
				{VendorID: "10", Name: "user-alice"},
				{VendorID: "11", Name: "user-bob"},
			},
			wantDeletions: 0,
			wantOutputs:   []string{"skipped (name)"},
		},
		{
			desc: "mixed user and c66 keys",
			keep: 1,
			keys: []core.SshKey{
				{VendorID: "1", Name: "c66-old"},
				{VendorID: "2", Name: "c66-newer"},
				{VendorID: "3", Name: "user-local"},
			},
			// 2 c66 keys, keep 1 → delete 1
			wantDeletions: 1,
			wantOutputs:   []string{"skipped (name)", "Deleted!", "skipped (keep last 1)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// real deletion path (not mock) so fakeExecutor.SshKeyDelete is called
			withFlags(t, false, flagMaxAgeNormal, flagMaxAgeLong)
			withSshKeepCount(t, tt.keep)

			fe := &fakeExecutor{}
			ctx := context.WithValue(context.Background(), core.ExecutorKey, core.ExecutorInterface(fe))
			got := captureOutput(t, func() {
				deleteSshKeys(ctx, tt.keys)
			})
			if len(fe.deletedKeys) != tt.wantDeletions {
				t.Errorf("deletions: want %d, got %d (keys: %v)", tt.wantDeletions, len(fe.deletedKeys), fe.deletedKeys)
			}
			for _, want := range tt.wantOutputs {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q; got:\n%s", want, got)
				}
			}
		})
	}
}

// TestDeleteSshKeys_MockOutput covers P3-T6: in mock mode the deletion prints
// "Mock deleted!" rather than really calling the executor.
func TestDeleteSshKeys_MockOutput(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)
	withSshKeepCount(t, 1)

	fe := &fakeExecutor{}
	ctx := context.WithValue(context.Background(), core.ExecutorKey, core.ExecutorInterface(fe))
	keys := []core.SshKey{
		{VendorID: "1", Name: "c66-first"},
		{VendorID: "2", Name: "c66-second"},
	}
	got := captureOutput(t, func() {
		deleteSshKeys(ctx, keys)
	})
	if !strings.Contains(got, "Mock deleted!") {
		t.Errorf("mock output missing 'Mock deleted!': %s", got)
	}
	if !strings.Contains(got, "skipped (keep last 1)") {
		t.Errorf("mock output missing keep reason: %s", got)
	}
	if len(fe.deletedKeys) != 0 {
		t.Errorf("mock mode must not call SshKeyDelete, got %d calls", len(fe.deletedKeys))
	}
}
