package executors

import (
	"context"
	"time"

	"github.com/cloud66/janitor/core"
	"github.com/vultr/govultr/v3"
	"golang.org/x/oauth2"
)

// Vultr encapsulates all Vultr cloud calls. Implements core.ExecutorInterface
// directly (no embedded base) for compile-time coverage.
type Vultr struct{}

// compile-time interface assertion
var _ core.ExecutorInterface = Vultr{}

// ServersGet returns all Vultr instances
func (v Vultr) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	client := v.client(ctx)

	var allInstances []govultr.Instance
	opts := &govultr.ListOptions{PerPage: 100}

	// paginate through all instances using cursor-based meta.Links.Next
	for {
		instances, meta, _, err := client.Instance.List(ctx, opts)
		if err != nil {
			return nil, err
		}

		allInstances = append(allInstances, instances...)

		// break when no more pages; meta may be nil on some error paths
		if meta == nil || meta.Links == nil || meta.Links.Next == "" {
			break
		}
		opts.Cursor = meta.Links.Next
	}

	result := make([]core.Server, 0, len(allInstances))
	for _, inst := range allInstances {
		// parse creation time; on parse failure log WARN and include with
		// Age=0 rather than aborting the whole listing (B10).
		var age float64
		if inst.DateCreated != "" {
			createdAt, err := time.Parse(time.RFC3339, inst.DateCreated)
			if err != nil {
				core.Warnf(ctx, "unparseable DateCreated %q for instance %q", inst.DateCreated, inst.Label)
			} else {
				age = time.Since(createdAt).Hours() / 24.0
			}
		}

		result = append(result, core.Server{
			VendorID: inst.ID,
			Name:     inst.Label,
			Age:      age,
			Region:   inst.Region,
			State:    "RUNNING",
			Tags:     inst.Tags,
		})
	}

	return result, nil
}

// ServerDelete removes the specified Vultr instance
func (v Vultr) ServerDelete(ctx context.Context, server core.Server) error {
	client := v.client(ctx)
	// vultr uses string IDs, no conversion needed
	return client.Instance.Delete(ctx, server.VendorID)
}

// ServerStop is unsupported on Vultr
func (v Vultr) ServerStop(ctx context.Context, server core.Server) error {
	return core.ErrUnsupported
}

// ServerStart is unsupported on Vultr
func (v Vultr) ServerStart(ctx context.Context, server core.Server) error {
	return core.ErrUnsupported
}

// LoadBalancersGet returns all Vultr load balancers with accurate instance counts
func (v Vultr) LoadBalancersGet(ctx context.Context, flagMock bool) ([]core.LoadBalancer, error) {
	client := v.client(ctx)

	var allLBs []govultr.LoadBalancer
	opts := &govultr.ListOptions{PerPage: 100}

	// paginate through all load balancers
	for {
		lbs, meta, _, err := client.LoadBalancer.List(ctx, opts)
		if err != nil {
			return nil, err
		}

		allLBs = append(allLBs, lbs...)

		if meta == nil || meta.Links == nil || meta.Links.Next == "" {
			break
		}
		opts.Cursor = meta.Links.Next
	}

	result := make([]core.LoadBalancer, 0, len(allLBs))
	for _, lb := range allLBs {
		var age float64
		if lb.DateCreated != "" {
			createdAt, err := time.Parse(time.RFC3339, lb.DateCreated)
			if err != nil {
				core.Warnf(ctx, "unparseable DateCreated %q for load balancer %q", lb.DateCreated, lb.Label)
			} else {
				age = time.Since(createdAt).Hours() / 24.0
			}
		}

		result = append(result, core.LoadBalancer{
			Name:            lb.Label,
			Age:             age,
			InstanceCount:   len(lb.Instances),
			Region:          lb.Region,
			Type:            "vultr",
			LoadBalancerArn: lb.ID, // repurpose ARN field to store Vultr LB ID
		})
	}

	return result, nil
}

// LoadBalancerDelete removes the specified Vultr load balancer
func (v Vultr) LoadBalancerDelete(ctx context.Context, loadBalancer core.LoadBalancer) error {
	client := v.client(ctx)
	// the Vultr LB ID is stored in LoadBalancerArn
	return client.LoadBalancer.Delete(ctx, loadBalancer.LoadBalancerArn)
}

// SshKeysGet is unsupported on Vultr today
func (v Vultr) SshKeysGet(ctx context.Context) ([]core.SshKey, error) {
	return nil, core.ErrUnsupported
}

// SshKeyDelete is unsupported on Vultr today
func (v Vultr) SshKeyDelete(ctx context.Context, sshKey core.SshKey) error {
	return core.ErrUnsupported
}

// VolumesGet returns all Vultr block storage volumes with attachment status
func (v Vultr) VolumesGet(ctx context.Context) ([]core.Volume, error) {
	client := v.client(ctx)

	var allVolumes []govultr.BlockStorage
	opts := &govultr.ListOptions{PerPage: 100}

	// paginate through all volumes
	for {
		volumes, meta, _, err := client.BlockStorage.List(ctx, opts)
		if err != nil {
			return nil, err
		}

		allVolumes = append(allVolumes, volumes...)

		if meta == nil || meta.Links == nil || meta.Links.Next == "" {
			break
		}
		opts.Cursor = meta.Links.Next
	}

	result := make([]core.Volume, 0, len(allVolumes))
	for _, vol := range allVolumes {
		var age float64
		if vol.DateCreated != "" {
			createdAt, err := time.Parse(time.RFC3339, vol.DateCreated)
			if err != nil {
				core.Warnf(ctx, "unparseable DateCreated %q for block storage %q", vol.DateCreated, vol.Label)
			} else {
				age = time.Since(createdAt).Hours() / 24.0
			}
		}

		// note: Vultr block storage has no tags concept, so Tags is always nil.
		result = append(result, core.Volume{
			VendorID: vol.ID,
			Name:     vol.Label,
			Age:      age,
			Region:   vol.Region,
			Attached: vol.AttachedToInstance != "",
		})
	}

	return result, nil
}

// VolumeDelete removes the specified Vultr block storage volume
func (v Vultr) VolumeDelete(ctx context.Context, volume core.Volume) error {
	client := v.client(ctx)
	return client.BlockStorage.Delete(ctx, volume.VendorID)
}

// client creates an authenticated Vultr API client. Credentials come from
// typed ctx key core.VultrPatKey. For tests, core.VultrBaseURLKey redirects
// the SDK to an httptest server via SetBaseURL.
func (v Vultr) client(ctx context.Context) *govultr.Client {
	apiKey, _ := ctx.Value(core.VultrPatKey).(string)
	// reuse the same oauth2 token pattern as DigitalOcean
	tokenSource := &TokenSource{AccessToken: apiKey}
	oauthClient := oauth2.NewClient(ctx, tokenSource)
	client := govultr.NewClient(oauthClient)
	if base, ok := ctx.Value(core.VultrBaseURLKey).(string); ok && base != "" {
		// SetBaseURL validates the URL; surface failures via Warnf so tests
		// redirecting to httptest still fail loudly instead of silently hitting
		// the real Vultr API. Default base URL stays active on failure.
		if err := client.SetBaseURL(base); err != nil {
			core.Warnf(ctx, "vultr SetBaseURL(%q) failed: %v", base, err)
		}
	}
	return client
}
