package executors

import (
	"time"

	"github.com/cloud66/janitor/core"
	"github.com/vultr/govultr/v3"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

// Vultr encapsulates all Vultr cloud calls
type Vultr struct {
	*core.Executor
}

// ServersGet returns all Vultr instances
func (v Vultr) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	client := v.client(ctx)

	var allInstances []govultr.Instance
	opts := &govultr.ListOptions{PerPage: 100}

	// paginate through all instances
	for {
		instances, meta, _, err := client.Instance.List(ctx, opts)
		if err != nil {
			return nil, err
		}

		allInstances = append(allInstances, instances...)

		// break when no more pages
		if meta.Links.Next == "" {
			break
		}
		opts.Cursor = meta.Links.Next
	}

	result := make([]core.Server, 0, len(allInstances))
	for _, inst := range allInstances {
		// parse creation time and calculate age in days
		createdAt, err := time.Parse(time.RFC3339, inst.DateCreated)
		if err != nil {
			return nil, err
		}
		age := time.Now().Sub(createdAt).Hours() / 24.0

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

		// break when no more pages
		if meta.Links.Next == "" {
			break
		}
		opts.Cursor = meta.Links.Next
	}

	result := make([]core.LoadBalancer, 0, len(allLBs))
	for _, lb := range allLBs {
		// parse creation time and calculate age in days
		createdAt, err := time.Parse(time.RFC3339, lb.DateCreated)
		if err != nil {
			return nil, err
		}
		age := time.Now().Sub(createdAt).Hours() / 24.0

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

		// break when no more pages
		if meta.Links.Next == "" {
			break
		}
		opts.Cursor = meta.Links.Next
	}

	result := make([]core.Volume, 0, len(allVolumes))
	for _, vol := range allVolumes {
		// parse creation time and calculate age in days
		createdAt, err := time.Parse(time.RFC3339, vol.DateCreated)
		if err != nil {
			return nil, err
		}
		age := time.Now().Sub(createdAt).Hours() / 24.0

		result = append(result, core.Volume{
			VendorID: vol.ID,
			Name:     vol.Label,
			Age:      age,
			Region:   vol.Region,
			Attached: vol.AttachedToInstance != "", // true if attached to any instance
		})
	}

	return result, nil
}

// VolumeDelete removes the specified Vultr block storage volume
func (v Vultr) VolumeDelete(ctx context.Context, volume core.Volume) error {
	client := v.client(ctx)
	return client.BlockStorage.Delete(ctx, volume.VendorID)
}

// client creates an authenticated Vultr API client
func (v *Vultr) client(ctx context.Context) *govultr.Client {
	apiKey := ctx.Value("JANITOR_VULTR_PAT").(string)
	// reuse the same oauth2 token pattern as DigitalOcean
	tokenSource := &TokenSource{
		AccessToken: apiKey,
	}
	oauthClient := oauth2.NewClient(ctx, tokenSource)
	return govultr.NewClient(oauthClient)
}
