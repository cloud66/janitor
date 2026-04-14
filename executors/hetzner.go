package executors

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloud66/janitor/core"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// Hetzner encapsulates all Hetzner Cloud calls. Implements
// core.ExecutorInterface directly (no embedded base) for compile-time coverage.
type Hetzner struct{}

// compile-time interface assertion
var _ core.ExecutorInterface = Hetzner{}

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
		// server.Created is a real time.Time (not a string), so no parse error
		// path — but guard against the zero value the same way the DO/Vultr
		// string parsers do: a zero Created yields Age=0 rather than a huge
		// number. (See B10 for the string-parse sibling behavior.)
		var age float64
		if !server.Created.IsZero() {
			age = time.Since(server.Created).Hours() / 24.0
		} else {
			core.Warnf(ctx, "missing Created for server %q", server.Name)
		}

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
	// parse the string vendor ID back to int64 strictly — no trailing garbage
	id, err := parseHetznerID(server.VendorID)
	if err != nil {
		return err
	}
	_, _, err = client.Server.DeleteWithResult(ctx, &hcloud.Server{ID: id})
	return err
}

// ServerStop is unsupported on Hetzner
func (h Hetzner) ServerStop(ctx context.Context, server core.Server) error {
	return core.ErrUnsupported
}

// ServerStart is unsupported on Hetzner
func (h Hetzner) ServerStart(ctx context.Context, server core.Server) error {
	return core.ErrUnsupported
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
		var age float64
		if !lb.Created.IsZero() {
			age = time.Since(lb.Created).Hours() / 24.0
		} else {
			core.Warnf(ctx, "missing Created for load balancer %q", lb.Name)
		}

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
	// fixes B9: fmt.Sscanf("%d", ...) accepted "123abc" and silently deleted
	// LB 123. parseHetznerID rejects any trailing non-digit characters.
	id, err := parseHetznerID(loadBalancer.LoadBalancerArn)
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
		var age float64
		if !vol.Created.IsZero() {
			age = time.Since(vol.Created).Hours() / 24.0
		} else {
			core.Warnf(ctx, "missing Created for volume %q", vol.Name)
		}

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

// SshKeysGet is unsupported on Hetzner today (no feature request yet)
func (h Hetzner) SshKeysGet(ctx context.Context) ([]core.SshKey, error) {
	return nil, core.ErrUnsupported
}

// SshKeyDelete is unsupported on Hetzner today
func (h Hetzner) SshKeyDelete(ctx context.Context, sshKey core.SshKey) error {
	return core.ErrUnsupported
}

// VolumeDelete removes the specified Hetzner Cloud volume
func (h Hetzner) VolumeDelete(ctx context.Context, volume core.Volume) error {
	client := h.client(ctx)
	id, err := parseHetznerID(volume.VendorID)
	if err != nil {
		return err
	}
	_, err = client.Volume.Delete(ctx, &hcloud.Volume{ID: id})
	return err
}

// parseHetznerID strictly parses a Hetzner resource ID. Unlike fmt.Sscanf with
// "%d", it rejects inputs that have any trailing non-digit garbage (e.g.
// "123abc") so we never silently act on a partial match — that was B9.
func parseHetznerID(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty hetzner id")
	}
	// reject leading '+' sign: strconv.ParseInt accepts it but Hetzner IDs
	// never carry a sign, so treat it as malformed input.
	if s[0] == '+' {
		return 0, fmt.Errorf("invalid hetzner id %q: leading sign not allowed", s)
	}
	// reject surrounding whitespace: ParseInt rejects it too, but be explicit
	// so the error message is consistent with the other rejections.
	if s != strings.TrimSpace(s) {
		return 0, fmt.Errorf("invalid hetzner id %q: whitespace not allowed", s)
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid hetzner id %q: %w", s, err)
	}
	// reject non-positive IDs — Hetzner resource IDs are always > 0.
	if id <= 0 {
		return 0, fmt.Errorf("invalid hetzner id %q: must be positive", s)
	}
	return id, nil
}

// hetznerLabelsToTags converts Hetzner's map[string]string labels to normalized
// "key=value" tag strings. The returned slice is sorted so callers (and tests)
// see a stable, deterministic ordering regardless of Go's random map iteration.
func hetznerLabelsToTags(labels map[string]string) []string {
	result := make([]string, 0, len(labels))
	for key, value := range labels {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	// sort lexicographically so output is stable across runs
	sort.Strings(result)
	return result
}

// client creates an authenticated Hetzner Cloud API client. Credentials come
// from typed ctx key core.HetznerPatKey. For tests, core.HetznerBaseURLKey
// redirects the SDK to an httptest server (must mount routes under /v1/...).
func (h Hetzner) client(ctx context.Context) *hcloud.Client {
	apiToken, _ := ctx.Value(core.HetznerPatKey).(string)
	opts := []hcloud.ClientOption{hcloud.WithToken(apiToken)}
	// test seam: if a base URL override is present, redirect the SDK to it.
	// hcloud.WithEndpoint doesn't validate, so we pre-parse and warn on
	// failure rather than silently hitting api.hetzner.cloud.
	if base, ok := ctx.Value(core.HetznerBaseURLKey).(string); ok && base != "" {
		if _, err := url.Parse(base); err == nil {
			opts = append(opts, hcloud.WithEndpoint(base))
		} else {
			core.Warnf(ctx, "hetzner BaseURL parse(%q) failed: %v", base, err)
		}
	}
	return hcloud.NewClient(opts...)
}
