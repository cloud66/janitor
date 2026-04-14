package executors

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/cloud66/janitor/core"
	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"
)

// TokenSource provides token transport for auth
type TokenSource struct {
	AccessToken string
}

// DigitalOcean encapsulates all DO cloud calls. It implements
// core.ExecutorInterface directly (no embedded base) so the compiler enforces
// coverage of every interface method.
type DigitalOcean struct{}

// compile-time interface assertion
var _ core.ExecutorInterface = DigitalOcean{}

// ServersGet returns collection of Server objects
func (d DigitalOcean) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	client := d.client(ctx)

	// paginate through all droplets — matches the pattern used by SshKeysGet.
	// fixes B11: previously a single unpaged List call silently truncated.
	allDroplets := []godo.Droplet{}
	opt := &godo.ListOptions{}
	for {
		droplets, resp, err := client.Droplets.List(ctx, opt)
		if err != nil {
			return nil, err
		}
		allDroplets = append(allDroplets, droplets...)

		// stop at the last page
		if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		page, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}
		opt.Page = page + 1
	}

	result := make([]core.Server, 0, len(allDroplets))
	for _, droplet := range allDroplets {
		createdAt := droplet.Created
		// parse creation timestamp; on parse failure we fall back to Age=0 and
		// log a warning rather than aborting the whole listing (B10).
		var age float64
		if createdAt != "" {
			createdAtDate, err := time.Parse(time.RFC3339, createdAt)
			if err != nil {
				core.Warnf(ctx, "unparseable Created %q for droplet %q", createdAt, droplet.Name)
			} else {
				age = time.Since(createdAtDate).Hours() / 24.0
			}
		}
		result = append(result, core.Server{VendorID: strconv.Itoa(droplet.ID), Name: droplet.Name, Age: age, Region: "Global", State: "RUNNING", Tags: droplet.Tags})
	}

	return result, nil
}

// ServerDelete remove the specified server
func (d DigitalOcean) ServerDelete(ctx context.Context, server core.Server) error {
	id, _ := strconv.Atoi(server.VendorID)
	_, err := d.client(ctx).Droplets.Delete(ctx, id)
	if err != nil {
		return err
	}
	return nil
}

// ServerStop is unsupported on DigitalOcean
func (d DigitalOcean) ServerStop(ctx context.Context, server core.Server) error {
	return core.ErrUnsupported
}

// ServerStart is unsupported on DigitalOcean
func (d DigitalOcean) ServerStart(ctx context.Context, server core.Server) error {
	return core.ErrUnsupported
}

// LoadBalancersGet returns all DigitalOcean load balancers with droplet counts
func (d DigitalOcean) LoadBalancersGet(ctx context.Context, flagMock bool) ([]core.LoadBalancer, error) {
	client := d.client(ctx)

	// collect all load balancers with pagination
	allLBs := []godo.LoadBalancer{}
	opt := &godo.ListOptions{}
	for {
		lbs, resp, err := client.LoadBalancers.List(ctx, opt)
		if err != nil {
			return nil, err
		}
		allLBs = append(allLBs, lbs...)

		// break at the last page
		if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		page, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}
		opt.Page = page + 1
	}

	// map all load balancers to core.LoadBalancer
	result := make([]core.LoadBalancer, 0, len(allLBs))
	for _, lb := range allLBs {
		// parse RFC3339 creation timestamp; on parse failure log WARN and
		// include with Age=0 instead of aborting (B10).
		var age float64
		if lb.Created != "" {
			createdAt, err := time.Parse(time.RFC3339, lb.Created)
			if err != nil {
				core.Warnf(ctx, "unparseable Created %q for load balancer %q", lb.Created, lb.Name)
			} else {
				age = time.Since(createdAt).Hours() / 24.0
			}
		}

		// instance count = explicit droplet IDs; tag-based LBs resolve droplets
		// server-side and aren't reflected in DropletIDs, so count may be 0 for those
		instanceCount := len(lb.DropletIDs)

		region := ""
		if lb.Region != nil {
			region = lb.Region.Slug
		}

		result = append(result, core.LoadBalancer{
			Name:            lb.Name,
			Age:             age,
			InstanceCount:   instanceCount,
			Region:          region,
			Type:            "do-lb",
			Tags:            lb.Tags,
			LoadBalancerArn: lb.ID, // repurpose ARN field for DO LB UUID
		})
	}

	return result, nil
}

// LoadBalancerDelete removes the specified DigitalOcean load balancer
func (d DigitalOcean) LoadBalancerDelete(ctx context.Context, loadBalancer core.LoadBalancer) error {
	// the DO LB UUID is stored in LoadBalancerArn
	_, err := d.client(ctx).LoadBalancers.Delete(ctx, loadBalancer.LoadBalancerArn)
	return err
}

// SshKeysGet gets SSH keys
func (d DigitalOcean) SshKeysGet(ctx context.Context) ([]core.SshKey, error) {
	doAllSshKeys := []godo.Key{}
	opt := &godo.ListOptions{}
	for {
		doSshKeys, resp, err := d.client(ctx).Keys.List(ctx, opt)
		if err != nil {
			return nil, err
		}

		// flatten the current page into the aggregate slice
		doAllSshKeys = append(doAllSshKeys, doSshKeys...)

		// If we are at the last page, break out the for loop
		if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		page, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}

		opt.Page = page + 1
	}

	result := make([]core.SshKey, 0, len(doAllSshKeys))
	for _, doSshKey := range doAllSshKeys {
		result = append(result, core.SshKey{VendorID: strconv.Itoa(doSshKey.ID), Name: doSshKey.Name})
	}

	return result, nil
}

// SshKeyDelete deletes an SSH key
func (d DigitalOcean) SshKeyDelete(ctx context.Context, sshKey core.SshKey) error {
	id, _ := strconv.Atoi(sshKey.VendorID)
	_, err := d.client(ctx).Keys.DeleteByID(ctx, id)
	if err != nil {
		return err
	}
	return nil
}

// VolumesGet returns unattached volumes
func (d DigitalOcean) VolumesGet(ctx context.Context) ([]core.Volume, error) {
	// collect all volumes with pagination
	allVolumes := []godo.Volume{}
	opt := &godo.ListVolumeParams{}
	for {
		doVolumes, resp, err := d.client(ctx).Storage.ListVolumes(ctx, opt)
		if err != nil {
			return nil, err
		}

		allVolumes = append(allVolumes, doVolumes...)

		// break at the last page
		if resp == nil || resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		page, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}

		opt.ListOptions = &godo.ListOptions{Page: page + 1}
	}

	// map all volumes to core.Volume with attachment status
	result := make([]core.Volume, 0, len(allVolumes))
	for _, vol := range allVolumes {
		age := time.Since(vol.CreatedAt).Hours() / 24.0
		region := ""
		if vol.Region != nil {
			region = vol.Region.Slug
		}
		result = append(result, core.Volume{
			VendorID: vol.ID,
			Name:     vol.Name,
			Age:      age,
			Region:   region,
			Attached: len(vol.DropletIDs) > 0,
			Tags:     vol.Tags,
		})
	}

	return result, nil
}

// VolumeDelete removes the specified volume
func (d DigitalOcean) VolumeDelete(ctx context.Context, volume core.Volume) error {
	_, err := d.client(ctx).Storage.DeleteVolume(ctx, volume.VendorID)
	return err
}

// Token retrieves the oauth token
func (t *TokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

// client builds an authenticated godo client. Credentials come from the typed
// context key core.DOPatKey. For tests, setting core.DOBaseURLKey points the
// SDK at an httptest server via godo's exported BaseURL field.
func (d DigitalOcean) client(ctx context.Context) *godo.Client {
	pat, _ := ctx.Value(core.DOPatKey).(string)
	tokenSource := &TokenSource{AccessToken: pat}
	// oauth2.NoContext is deprecated; use context.Background for the same effect.
	oauthClient := oauth2.NewClient(context.Background(), tokenSource)
	client := godo.NewClient(oauthClient)
	// test seam: if a base URL override is present, redirect the SDK to it.
	if base, ok := ctx.Value(core.DOBaseURLKey).(string); ok && base != "" {
		if u, err := url.Parse(base + "/"); err == nil {
			client.BaseURL = u
		}
	}
	return client
}
