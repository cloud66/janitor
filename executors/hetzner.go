package executors

import (
	"fmt"
	"time"

	"github.com/cloud66/janitor/core"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"golang.org/x/net/context"
)

// Hetzner encapsulates all Hetzner Cloud calls
type Hetzner struct {
	*core.Executor
}

// ServersGet returns all Hetzner Cloud servers
func (h Hetzner) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	client := h.client(ctx)

	// All() handles pagination internally
	servers, err := client.Server.All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]core.Server, 0, len(servers))
	for _, server := range servers {
		age := time.Now().Sub(server.Created).Hours() / 24.0

		// map hetzner status to our state format
		state := "RUNNING"
		if server.Status != hcloud.ServerStatusRunning {
			state = string(server.Status)
		}

		// guard against nil Datacenter in case of incomplete API response
		region := ""
		if server.Datacenter != nil {
			region = server.Datacenter.Name
		}

		result = append(result, core.Server{
			VendorID: fmt.Sprintf("%d", server.ID),
			Name:     server.Name,
			Age:      age,
			Region:   region,
			State:    state,
			Tags:     hetznerLabelsToTags(server.Labels),
		})
	}

	return result, nil
}

// ServerDelete removes the specified Hetzner Cloud server
func (h Hetzner) ServerDelete(ctx context.Context, server core.Server) error {
	client := h.client(ctx)
	// parse the string vendor ID back to int64
	var id int64
	_, err := fmt.Sscanf(server.VendorID, "%d", &id)
	if err != nil {
		return err
	}
	_, _, err = client.Server.DeleteWithResult(ctx, &hcloud.Server{ID: id})
	return err
}

// LoadBalancersGet returns all Hetzner Cloud load balancers with target counts
func (h Hetzner) LoadBalancersGet(ctx context.Context, flagMock bool) ([]core.LoadBalancer, error) {
	client := h.client(ctx)

	// All() handles pagination internally
	lbs, err := client.LoadBalancer.All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]core.LoadBalancer, 0, len(lbs))
	for _, lb := range lbs {
		age := time.Now().Sub(lb.Created).Hours() / 24.0

		// count targets across all target types (server, label-selector, IP)
		instanceCount := 0
		for _, target := range lb.Targets {
			switch target.Type {
			case hcloud.LoadBalancerTargetTypeServer:
				if target.Server != nil {
					instanceCount++
				}
			case hcloud.LoadBalancerTargetTypeLabelSelector:
				// label selectors may temporarily resolve to 0 servers (autoscaling, deployments)
				// count at least 1 since the selector's existence means the LB is configured for traffic
				if len(target.Targets) > 0 {
					instanceCount += len(target.Targets)
				} else {
					instanceCount++
				}
			case hcloud.LoadBalancerTargetTypeIP:
				instanceCount++
			}
		}

		// guard against nil Location in case of incomplete API response
		region := ""
		if lb.Location != nil {
			region = lb.Location.Name
		}

		result = append(result, core.LoadBalancer{
			Name:            lb.Name,
			Age:             age,
			InstanceCount:   instanceCount,
			Region:          region,
			Type:            "hetzner",
			Tags:            hetznerLabelsToTags(lb.Labels),
			LoadBalancerArn: fmt.Sprintf("%d", lb.ID), // repurpose ARN field for Hetzner LB ID
		})
	}

	return result, nil
}

// LoadBalancerDelete removes the specified Hetzner Cloud load balancer
func (h Hetzner) LoadBalancerDelete(ctx context.Context, loadBalancer core.LoadBalancer) error {
	client := h.client(ctx)
	var id int64
	_, err := fmt.Sscanf(loadBalancer.LoadBalancerArn, "%d", &id)
	if err != nil {
		return err
	}
	_, err = client.LoadBalancer.Delete(ctx, &hcloud.LoadBalancer{ID: id})
	return err
}

// VolumesGet returns all Hetzner Cloud volumes with attachment status
func (h Hetzner) VolumesGet(ctx context.Context) ([]core.Volume, error) {
	client := h.client(ctx)

	// All() handles pagination internally
	volumes, err := client.Volume.All(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]core.Volume, 0, len(volumes))
	for _, vol := range volumes {
		age := time.Now().Sub(vol.Created).Hours() / 24.0

		// guard against nil Location in case of incomplete API response
		region := ""
		if vol.Location != nil {
			region = vol.Location.Name
		}

		result = append(result, core.Volume{
			VendorID: fmt.Sprintf("%d", vol.ID),
			Name:     vol.Name,
			Age:      age,
			Region:   region,
			Attached: vol.Server != nil, // nil means unattached
			Tags:     hetznerLabelsToTags(vol.Labels),
		})
	}

	return result, nil
}

// VolumeDelete removes the specified Hetzner Cloud volume
func (h Hetzner) VolumeDelete(ctx context.Context, volume core.Volume) error {
	client := h.client(ctx)
	var id int64
	_, err := fmt.Sscanf(volume.VendorID, "%d", &id)
	if err != nil {
		return err
	}
	_, err = client.Volume.Delete(ctx, &hcloud.Volume{ID: id})
	return err
}

// hetznerLabelsToTags converts Hetzner's map[string]string labels to normalized "key=value" tag strings
func hetznerLabelsToTags(labels map[string]string) []string {
	result := make([]string, 0, len(labels))
	for key, value := range labels {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	return result
}

// client creates an authenticated Hetzner Cloud API client
func (h *Hetzner) client(ctx context.Context) *hcloud.Client {
	apiToken := ctx.Value("JANITOR_HETZNER_PAT").(string)
	return hcloud.NewClient(hcloud.WithToken(apiToken))
}
