package executors

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloud66/janitor/core"
	"golang.org/x/net/context"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// GCP encapsulates all Google Cloud Platform Compute Engine calls
type GCP struct {
	*core.Executor
}

// ServersGet returns all GCP Compute Engine instances across all zones
func (g GCP) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	service, project, err := g.client(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]core.Server, 0)

	// aggregated list returns instances grouped by zone across the entire project
	req := service.Instances.AggregatedList(project)
	err = req.Pages(ctx, func(page *compute.InstanceAggregatedList) error {
		for zone, scopedList := range page.Items {
			for _, instance := range scopedList.Instances {
				// parse the RFC3339 creation timestamp
				created, parseErr := time.Parse(time.RFC3339, instance.CreationTimestamp)
				if parseErr != nil {
					continue
				}
				age := time.Since(created).Hours() / 24.0

				// map GCP status to our state format
				state := "RUNNING"
				if instance.Status != "RUNNING" {
					state = instance.Status
				}

				// extract zone name from "zones/us-central1-a" format
				zoneName := zone
				if strings.HasPrefix(zone, "zones/") {
					zoneName = strings.TrimPrefix(zone, "zones/")
				}

				result = append(result, core.Server{
					VendorID: fmt.Sprintf("%d", instance.Id),
					Name:     instance.Name,
					Age:      age,
					Region:   zoneName,
					State:    state,
					Tags:     gcpLabelsToTags(instance.Labels),
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ServerDelete removes the specified GCP Compute Engine instance
func (g GCP) ServerDelete(ctx context.Context, server core.Server) error {
	service, project, err := g.client(ctx)
	if err != nil {
		return err
	}
	// Region field contains the zone name set during ServersGet
	_, err = service.Instances.Delete(project, server.Region, server.Name).Context(ctx).Do()
	return err
}

// VolumesGet returns all GCP persistent disks across all zones
func (g GCP) VolumesGet(ctx context.Context) ([]core.Volume, error) {
	service, project, err := g.client(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]core.Volume, 0)

	// aggregated list returns disks grouped by zone
	req := service.Disks.AggregatedList(project)
	err = req.Pages(ctx, func(page *compute.DiskAggregatedList) error {
		for zone, scopedList := range page.Items {
			for _, disk := range scopedList.Disks {
				// parse the RFC3339 creation timestamp
				created, parseErr := time.Parse(time.RFC3339, disk.CreationTimestamp)
				if parseErr != nil {
					continue
				}
				age := time.Since(created).Hours() / 24.0

				// extract zone name from "zones/us-central1-a" format
				zoneName := zone
				if strings.HasPrefix(zone, "zones/") {
					zoneName = strings.TrimPrefix(zone, "zones/")
				}

				result = append(result, core.Volume{
					VendorID: fmt.Sprintf("%d", disk.Id),
					Name:     disk.Name,
					Age:      age,
					Region:   zoneName,
					Attached: len(disk.Users) > 0, // Users lists instances using this disk
					Tags:     gcpLabelsToTags(disk.Labels),
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// VolumeDelete removes the specified GCP persistent disk
func (g GCP) VolumeDelete(ctx context.Context, volume core.Volume) error {
	service, project, err := g.client(ctx)
	if err != nil {
		return err
	}
	// Region field contains the zone name set during VolumesGet
	_, err = service.Disks.Delete(project, volume.Region, volume.Name).Context(ctx).Do()
	return err
}

// gcpLabelsToTags converts GCP's map[string]string labels to normalized "key=value" tag strings
func gcpLabelsToTags(labels map[string]string) []string {
	result := make([]string, 0, len(labels))
	for key, value := range labels {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	return result
}

// client creates an authenticated GCP Compute Engine service client.
// If a credentials file is provided via context, it uses that; otherwise falls back to application default credentials.
func (g *GCP) client(ctx context.Context) (*compute.Service, string, error) {
	credentialsFile := ctx.Value("JANITOR_GCP_CREDENTIALS").(string)
	project := ctx.Value("JANITOR_GCP_PROJECT").(string)

	var service *compute.Service
	var err error

	if credentialsFile != "" {
		// use explicit service account credentials file
		service, err = compute.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	} else {
		// fall back to application default credentials (ADC)
		service, err = compute.NewService(ctx)
	}
	if err != nil {
		return nil, "", err
	}

	return service, project, nil
}
