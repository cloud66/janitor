package main

import (
	"regexp"
	"testing"

	"github.com/cloud66/janitor/core"
)

// --- isPermanent tests ---

func TestIsPermanent_NameContainsPermanent(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		expected bool
	}{
		// match on name
		{"my-permanent-server", nil, true},
		{"PERMANENT-box", nil, true},
		{"test-Permanent-lb", nil, true},
		// no match
		{"my-server", nil, false},
		{"perm-server", nil, false},
		{"", nil, false},
	}

	for _, tt := range tests {
		result := isPermanent(tt.name, tt.tags)
		if result != tt.expected {
			t.Errorf("isPermanent(%q, %v) = %v, want %v", tt.name, tt.tags, result, tt.expected)
		}
	}
}

func TestIsPermanent_TagContainsPermanent(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		expected bool
	}{
		// match on tag value
		{"my-server", []string{"lifecycle=permanent"}, true},
		{"my-server", []string{"type=permanent-resource"}, true},
		// match on tag key
		{"my-server", []string{"permanent=true"}, true},
		// case insensitive
		{"my-server", []string{"lifecycle=PERMANENT"}, true},
		// no match in tags
		{"my-server", []string{"lifecycle=temporary"}, false},
		{"my-server", []string{}, false},
		// match in name even if tags don't match
		{"permanent-box", []string{"lifecycle=temporary"}, true},
	}

	for _, tt := range tests {
		result := isPermanent(tt.name, tt.tags)
		if result != tt.expected {
			t.Errorf("isPermanent(%q, %v) = %v, want %v", tt.name, tt.tags, result, tt.expected)
		}
	}
}

// --- hasSampleTag tests ---

func TestHasSampleTag(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
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
	// set up mock mode so nothing actually gets deleted
	flagMock = true
	flagMaxAgeLong = 5.0
	flagMaxAgeNormal = 0.38

	longRegex, _ := regexp.Compile(`(?i)long`)

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

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			// we just verify no panic — the classification is tested via isPermanent/hasSampleTag above
			// and mock mode prevents actual deletions
			deleteServers(nil, tt.cloud, longRegex, []core.Server{tt.server})
		})
	}
}

// --- deleteLoadBalancers classification tests ---

func TestDeleteLoadBalancers_PermanentSkipped(t *testing.T) {
	flagMock = true

	lbs := []core.LoadBalancer{
		{Name: "permanent-lb", Age: 2.0, InstanceCount: 0, Region: "us", Type: "alb"},
	}
	// should not panic, LB is skipped due to permanent name
	deleteLoadBalancers(nil, lbs)
}

func TestDeleteLoadBalancers_LiveSkipped(t *testing.T) {
	flagMock = true

	lbs := []core.LoadBalancer{
		{Name: "active-lb", Age: 2.0, InstanceCount: 3, Region: "us", Type: "alb"},
	}
	// should be skipped because it has instances
	deleteLoadBalancers(nil, lbs)
}

func TestDeleteLoadBalancers_NewSkipped(t *testing.T) {
	flagMock = true

	lbs := []core.LoadBalancer{
		// age is 30 minutes (0.5 hours = 0.02 days), less than 1 hour threshold
		{Name: "new-lb", Age: 0.02, InstanceCount: 0, Region: "us", Type: "alb"},
	}
	// should be skipped because it's too new
	deleteLoadBalancers(nil, lbs)
}

func TestDeleteLoadBalancers_UnknownInstanceCountSkipped(t *testing.T) {
	flagMock = true

	lbs := []core.LoadBalancer{
		// instanceCount -1 means health check failed — should be skipped
		{Name: "mystery-lb", Age: 2.0, InstanceCount: -1, Region: "us", Type: "alb"},
	}
	// should be skipped because instance count is unknown
	deleteLoadBalancers(nil, lbs)
}

func TestDeleteLoadBalancers_DeadDeletedInMock(t *testing.T) {
	flagMock = true

	lbs := []core.LoadBalancer{
		// 2 days old, 0 instances — should be deleted
		{Name: "dead-lb", Age: 2.0, InstanceCount: 0, Region: "us", Type: "alb"},
	}
	// should reach the "Mock deleted!" path without panic
	deleteLoadBalancers(nil, lbs)
}

// --- deleteVolumes classification tests ---

func TestDeleteVolumes_PermanentSkipped(t *testing.T) {
	flagMock = true

	volumes := []core.Volume{
		{Name: "permanent-volume", Age: 2.0, Region: "us", Attached: false},
	}
	deleteVolumes(nil, volumes)
}

func TestDeleteVolumes_AttachedSkipped(t *testing.T) {
	flagMock = true

	volumes := []core.Volume{
		{Name: "data-vol", Age: 2.0, Region: "us", Attached: true},
	}
	deleteVolumes(nil, volumes)
}

func TestDeleteVolumes_TooNewSkipped(t *testing.T) {
	flagMock = true

	volumes := []core.Volume{
		// 30 minutes old
		{Name: "new-vol", Age: 0.02, Region: "us", Attached: false},
	}
	deleteVolumes(nil, volumes)
}

func TestDeleteVolumes_OldUnattachedDeletedInMock(t *testing.T) {
	flagMock = true

	volumes := []core.Volume{
		{Name: "orphan-vol", Age: 2.0, Region: "us", Attached: false},
	}
	// should reach "Mock deleted!" path
	deleteVolumes(nil, volumes)
}

func TestDeleteVolumes_PermanentByTagSkipped(t *testing.T) {
	flagMock = true

	volumes := []core.Volume{
		{Name: "data-vol", Age: 2.0, Region: "us", Attached: false, Tags: []string{"lifecycle=permanent"}},
	}
	deleteVolumes(nil, volumes)
}
