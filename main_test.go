package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cloud66/janitor/core"
)

// captureOutput swaps the package-level out sink for a bytes.Buffer, runs fn,
// restores the original sink via t.Cleanup, and returns the captured output.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	// save previous sink so we can restore it after the test
	prev := out
	buf := &bytes.Buffer{}
	out = buf
	t.Cleanup(func() {
		out = prev
	})
	fn()
	return buf.String()
}

// withFlags sets the global test-affecting flags and registers a t.Cleanup
// to restore their prior values. Use this instead of mutating flagMock /
// flagMaxAgeNormal / flagMaxAgeLong directly so tests don't leak state
// across runs (especially with -shuffle=on).
func withFlags(t *testing.T, mock bool, normal, long float64) {
	t.Helper()
	// capture previous values
	prevMock := flagMock
	prevNormal := flagMaxAgeNormal
	prevLong := flagMaxAgeLong
	// apply new values
	flagMock = mock
	flagMaxAgeNormal = normal
	flagMaxAgeLong = long
	// restore on test end
	t.Cleanup(func() {
		flagMock = prevMock
		flagMaxAgeNormal = prevNormal
		flagMaxAgeLong = prevLong
	})
}

// --- isPermanent tests ---

func TestIsPermanent_NameContainsPermanent(t *testing.T) {
	// pure-function test — safe to run in parallel
	t.Parallel()
	tests := []struct {
		desc     string
		name     string
		tags     []string
		expected bool
	}{
		// match on name
		{"lowercase permanent in name", "my-permanent-server", nil, true},
		{"uppercase PERMANENT in name", "PERMANENT-box", nil, true},
		{"mixed case Permanent in name", "test-Permanent-lb", nil, true},
		// no match
		{"plain name no match", "my-server", nil, false},
		{"prefix perm not full word", "perm-server", nil, false},
		{"empty name", "", nil, false},
		// B4: pins current greedy substring; fix tracked as B4.
		{"substring false positive supermanent", "supermanent-name", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			result := isPermanent(tt.name, tt.tags)
			if result != tt.expected {
				t.Errorf("isPermanent(%q, %v) = %v, want %v", tt.name, tt.tags, result, tt.expected)
			}
		})
	}
}

func TestIsPermanent_TagContainsPermanent(t *testing.T) {
	// pure-function test — safe to run in parallel
	t.Parallel()
	tests := []struct {
		desc     string
		name     string
		tags     []string
		expected bool
	}{
		// match on tag value
		{"tag value permanent", "my-server", []string{"lifecycle=permanent"}, true},
		{"tag value permanent-resource", "my-server", []string{"type=permanent-resource"}, true},
		// match on tag key
		{"tag key permanent", "my-server", []string{"permanent=true"}, true},
		// case insensitive
		{"tag value uppercase PERMANENT", "my-server", []string{"lifecycle=PERMANENT"}, true},
		// no match in tags
		{"tag value temporary no match", "my-server", []string{"lifecycle=temporary"}, false},
		{"empty tags no match", "my-server", []string{}, false},
		// match in name even if tags don't match
		{"name match overrides tag miss", "permanent-box", []string{"lifecycle=temporary"}, true},
		// B4: pins current greedy substring; fix tracked as B4.
		{"tag value permanent-core in C66 key", "my-server", []string{"env=prod", "C66=permanent-core"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			result := isPermanent(tt.name, tt.tags)
			if result != tt.expected {
				t.Errorf("isPermanent(%q, %v) = %v, want %v", tt.name, tt.tags, result, tt.expected)
			}
		})
	}
}

// --- hasLongName tests ---

func TestHasLongName(t *testing.T) {
	// pure-function test — safe to run in parallel
	t.Parallel()
	tests := []struct {
		desc     string
		name     string
		tags     []string
		expected bool
	}{
		// match on name
		{"name contains long", "my-long-running-job", nil, true},
		{"name contains LONG uppercase", "LONG-server", nil, true},
		{"name contains Long mixed case", "test-Long-task", nil, true},
		// no match on name
		{"name without long", "my-server", nil, false},
		{"empty name", "", nil, false},
		// match on tag
		{"tag value contains long", "my-server", []string{"lifecycle=long-running"}, true},
		{"tag key contains long", "my-server", []string{"long-lived=true"}, true},
		// no match on tags
		{"tag without long", "my-server", []string{"lifecycle=temporary"}, false},
		{"empty tags", "my-server", []string{}, false},
		{"nil tags", "my-server", nil, false},
		// name match overrides tag miss
		{"long in name, no tag match", "long-box", []string{"env=prod"}, true},
		// B4: pins current greedy substring; fix tracked as B4.
		{"substring false positive prolonged", "prolonged-task", nil, true},
		{"substring false positive belonging", "belonging", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			result := hasLongName(tt.name, tt.tags)
			if result != tt.expected {
				t.Errorf("hasLongName(%q, %v) = %v, want %v", tt.name, tt.tags, result, tt.expected)
			}
		})
	}
}

// --- hasSampleTag tests ---

func TestHasSampleTag(t *testing.T) {
	// pure-function test — safe to run in parallel
	t.Parallel()
	tests := []struct {
		desc     string
		tags     []string
		expected bool
	}{
		{
			desc:     "matches C66-STACK with sample in value",
			tags:     []string{"C66-STACK=maestro-sample-prd"},
			expected: true,
		},
		{
			desc:     "case insensitive key",
			tags:     []string{"c66-stack=maestro-sample-prd"},
			expected: true,
		},
		{
			desc:     "case insensitive value",
			tags:     []string{"C66-STACK=MAESTRO-SAMPLE-PRD"},
			expected: true,
		},
		{
			desc:     "no match without sample in value",
			tags:     []string{"C66-STACK=maestro-production-prd"},
			expected: false,
		},
		{
			desc:     "no match with different key",
			tags:     []string{"OTHER-KEY=maestro-sample-prd"},
			expected: false,
		},
		{
			desc:     "no match with empty tags",
			tags:     []string{},
			expected: false,
		},
		{
			desc:     "no match with nil tags",
			tags:     nil,
			expected: false,
		},
		{
			desc:     "matches among multiple tags",
			tags:     []string{"env=staging", "C66-STACK=test-sample-app", "team=dev"},
			expected: true,
		},
		// B3 FIXED: tag without "=" is not a key=value tag and is ignored.
		{
			desc:     "tag with no equals sign — no match",
			tags:     []string{"c66-stack"},
			expected: false,
		},
		// B3 FIXED: whitespace around the key is now trimmed before compare.
		{
			desc:     "whitespace around key — now matches after B3 fix",
			tags:     []string{" C66-STACK=x-sample"},
			expected: true,
		},
		// B3 FIXED: "sample" in a different key's key-portion must NOT trigger;
		// only the value of c66-stack is scanned. Pre-fix this was coincidentally
		// correct but for the wrong reason; post-fix it's structurally correct.
		{
			desc:     "sample in a different key portion — no match",
			tags:     []string{"samplekey=x", "c66-stack=nope"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			t.Parallel()
			result := hasSampleTag(tt.tags)
			if result != tt.expected {
				t.Errorf("hasSampleTag(%v) = %v, want %v", tt.tags, result, tt.expected)
			}
		})
	}
}

// --- deleteServers classification tests ---
// these test the classification logic by running in mock mode and checking output

func TestDeleteServers_Classification(t *testing.T) {
	// mutates global flags — must not be parallel; withFlags restores on cleanup
	withFlags(t, true, 0.38, 5.0)

	tests := []struct {
		desc           string
		cloud          string
		server         core.Server
		expectCategory string // PERM, SMPL, LONG, NORM
	}{
		{
			desc:           "permanent server by name",
			cloud:          "vultr",
			server:         core.Server{Name: "my-permanent-server", Age: 10, Region: "us", State: "RUNNING"},
			expectCategory: "PERM",
		},
		{
			desc:           "permanent server by tag",
			cloud:          "vultr",
			server:         core.Server{Name: "my-server", Age: 10, Region: "us", State: "RUNNING", Tags: []string{"lifecycle=permanent"}},
			expectCategory: "PERM",
		},
		{
			desc:           "vultr sample server skipped",
			cloud:          "vultr",
			server:         core.Server{Name: "test-app", Age: 10, Region: "us", State: "RUNNING", Tags: []string{"C66-STACK=maestro-sample-prd"}},
			expectCategory: "SMPL",
		},
		{
			desc:           "sample tag ignored for non-vultr",
			cloud:          "aws",
			server:         core.Server{Name: "test-app", Age: 10, Region: "us", State: "RUNNING", Tags: []string{"C66-STACK=maestro-sample-prd"}},
			expectCategory: "NORM", // not SMPL — hasSampleTag only applies to vultr
		},
		{
			desc:           "long server",
			cloud:          "aws",
			server:         core.Server{Name: "my-long-running-job", Age: 10, Region: "us", State: "RUNNING"},
			expectCategory: "LONG",
		},
		{
			desc:           "normal server",
			cloud:          "aws",
			server:         core.Server{Name: "my-server", Age: 10, Region: "us", State: "RUNNING"},
			expectCategory: "NORM",
		},
	}

	// all possible category tags — used for negative assertions
	allCategories := []string{"PERM", "SMPL", "LONG", "NORM"}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// capture printed output and assert the classification tag shows up
			got := captureOutput(t, func() {
				deleteServers(nil, tt.cloud, []core.Server{tt.server})
			})
			// anchor with bracket-space-bracket on BOTH sides so a loose
			// substring like "[PERM]" embedded in a name cannot match
			want := "] [" + tt.expectCategory + "] ["
			if !strings.Contains(got, want) {
				t.Errorf("expected output to contain %q, got %q", want, got)
			}
			// negative assertion: none of the OTHER category tags appear in
			// the slot — catches mis-classifications and loose substring hits
			for _, cat := range allCategories {
				if cat == tt.expectCategory {
					continue
				}
				bad := "] [" + cat + "] ["
				if strings.Contains(got, bad) {
					t.Errorf("unexpected category tag %q in output %q", bad, got)
				}
			}
		})
	}
}

// --- deleteLoadBalancers classification tests ---

func TestDeleteLoadBalancers_PermanentSkipped(t *testing.T) {
	// mutates flagMock; withFlags restores after the test
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	lbs := []core.LoadBalancer{
		{Name: "permanent-lb", Age: 2.0, InstanceCount: 0, Region: "us", Type: "alb"},
	}
	// capture output and assert the permanent skip reason appears
	got := captureOutput(t, func() {
		deleteLoadBalancers(nil, lbs)
	})
	// anchor the state tag with bracket-space-bracket on both sides
	if !strings.Contains(got, "] [PERM] [") {
		t.Errorf("expected output to contain \"] [PERM] [\", got %q", got)
	}
	// negative assertions: no OTHER LB state tag should appear in the slot
	for _, bad := range []string{"] [LIVE] [", "] [ NEW] [", "] [ N/A] [", "] [DEAD] ["} {
		if strings.Contains(got, bad) {
			t.Errorf("unexpected state tag %q in output %q", bad, got)
		}
	}
	if !strings.Contains(got, "skipped (permanent)") {
		t.Errorf("expected output to contain skipped (permanent), got %q", got)
	}
}

func TestDeleteLoadBalancers_LiveSkipped(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	lbs := []core.LoadBalancer{
		{Name: "active-lb", Age: 2.0, InstanceCount: 3, Region: "us", Type: "alb"},
	}
	// should be skipped because it has instances — the message includes the count
	got := captureOutput(t, func() {
		deleteLoadBalancers(nil, lbs)
	})
	if !strings.Contains(got, "skipped (has 3 instances)") {
		t.Errorf("expected output to contain skipped (has 3 instances), got %q", got)
	}
}

func TestDeleteLoadBalancers_NewSkipped(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	lbs := []core.LoadBalancer{
		// age is 30 minutes (0.5 hours = 0.02 days), less than 1 hour threshold
		{Name: "new-lb", Age: 0.02, InstanceCount: 0, Region: "us", Type: "alb"},
	}
	got := captureOutput(t, func() {
		deleteLoadBalancers(nil, lbs)
	})
	if !strings.Contains(got, "less than 1 hour old") {
		t.Errorf("expected output to contain less than 1 hour old, got %q", got)
	}
}

// TestDeleteLoadBalancers_AgeBoundary probes values either side of the
// 1-hour-in-days threshold (1.0/24.0 ≈ 0.04167) to pin the boundary
// semantics. 0.04 is just below (~57.6m), 0.05 is just above (72m).
func TestDeleteLoadBalancers_AgeBoundary(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	t.Run("just below threshold still too new", func(t *testing.T) {
		// 0.04 days ≈ 57.6 minutes, still < 1 hour threshold
		lbs := []core.LoadBalancer{
			{Name: "near-new-lb", Age: 0.04, InstanceCount: 0, Region: "us", Type: "alb"},
		}
		got := captureOutput(t, func() {
			deleteLoadBalancers(nil, lbs)
		})
		if !strings.Contains(got, "less than 1 hour old") {
			t.Errorf("expected 'less than 1 hour old' at Age=0.04, got %q", got)
		}
		// must not be classified as DEAD
		if strings.Contains(got, "] [DEAD] [") {
			t.Errorf("unexpected DEAD classification at Age=0.04, got %q", got)
		}
	})

	t.Run("just above threshold eligible for delete", func(t *testing.T) {
		// 0.05 days = 72 minutes, > 1 hour threshold; 0 instances → DEAD path
		lbs := []core.LoadBalancer{
			{Name: "just-old-lb", Age: 0.05, InstanceCount: 0, Region: "us", Type: "alb"},
		}
		got := captureOutput(t, func() {
			deleteLoadBalancers(nil, lbs)
		})
		// should NOT be skipped as too new
		if strings.Contains(got, "less than 1 hour old") {
			t.Errorf("did not expect 'less than 1 hour old' at Age=0.05, got %q", got)
		}
		// should proceed to delete path in mock mode
		if !strings.Contains(got, "Mock deleted!") {
			t.Errorf("expected 'Mock deleted!' at Age=0.05 with 0 instances, got %q", got)
		}
	})
}

func TestDeleteLoadBalancers_UnknownInstanceCountSkipped(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	lbs := []core.LoadBalancer{
		// instanceCount -1 means health check failed — should be skipped
		{Name: "mystery-lb", Age: 2.0, InstanceCount: -1, Region: "us", Type: "alb"},
	}
	got := captureOutput(t, func() {
		deleteLoadBalancers(nil, lbs)
	})
	if !strings.Contains(got, "instance count unknown") {
		t.Errorf("expected output to contain instance count unknown, got %q", got)
	}
}

func TestDeleteLoadBalancers_DeadDeletedInMock(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	lbs := []core.LoadBalancer{
		// 2 days old, 0 instances — should be deleted
		{Name: "dead-lb", Age: 2.0, InstanceCount: 0, Region: "us", Type: "alb"},
	}
	got := captureOutput(t, func() {
		deleteLoadBalancers(nil, lbs)
	})
	if !strings.Contains(got, "Mock deleted!") {
		t.Errorf("expected output to contain Mock deleted!, got %q", got)
	}
}

// --- deleteVolumes classification tests ---

func TestDeleteVolumes_PermanentSkipped(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	volumes := []core.Volume{
		{Name: "permanent-volume", Age: 2.0, Region: "us", Attached: false},
	}
	got := captureOutput(t, func() {
		deleteVolumes(nil, volumes)
	})
	if !strings.Contains(got, "skipped (permanent)") {
		t.Errorf("expected output to contain skipped (permanent), got %q", got)
	}
}

func TestDeleteVolumes_AttachedSkipped(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	volumes := []core.Volume{
		{Name: "data-vol", Age: 2.0, Region: "us", Attached: true},
	}
	got := captureOutput(t, func() {
		deleteVolumes(nil, volumes)
	})
	if !strings.Contains(got, "skipped (attached to instance)") {
		t.Errorf("expected output to contain skipped (attached to instance), got %q", got)
	}
}

func TestDeleteVolumes_TooNewSkipped(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	volumes := []core.Volume{
		// 30 minutes old
		{Name: "new-vol", Age: 0.02, Region: "us", Attached: false},
	}
	got := captureOutput(t, func() {
		deleteVolumes(nil, volumes)
	})
	if !strings.Contains(got, "skipped (too new)") {
		t.Errorf("expected output to contain skipped (too new), got %q", got)
	}
}

func TestDeleteVolumes_OldUnattachedDeletedInMock(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	volumes := []core.Volume{
		{Name: "orphan-vol", Age: 2.0, Region: "us", Attached: false},
	}
	got := captureOutput(t, func() {
		deleteVolumes(nil, volumes)
	})
	if !strings.Contains(got, "Mock deleted!") {
		t.Errorf("expected output to contain Mock deleted!, got %q", got)
	}
}

func TestDeleteVolumes_PermanentByTagSkipped(t *testing.T) {
	withFlags(t, true, flagMaxAgeNormal, flagMaxAgeLong)

	volumes := []core.Volume{
		{Name: "data-vol", Age: 2.0, Region: "us", Attached: false, Tags: []string{"lifecycle=permanent"}},
	}
	got := captureOutput(t, func() {
		deleteVolumes(nil, volumes)
	})
	if !strings.Contains(got, "skipped (permanent)") {
		t.Errorf("expected output to contain skipped (permanent), got %q", got)
	}
}
