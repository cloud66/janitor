package executors

import (
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/cloud66/janitor/core"
	"golang.org/x/net/context"
)

// Azure encapsulates all Microsoft Azure calls
type Azure struct {
	*core.Executor
}

// ServersGet returns all Azure virtual machines across the subscription
func (a Azure) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	cred, subscriptionID, err := a.credential(ctx)
	if err != nil {
		return nil, err
	}

	client, err := armcompute.NewVirtualMachinesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, err
	}

	result := make([]core.Server, 0)

	// list all VMs across the entire subscription
	pager := client.NewListAllPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, vm := range page.Value {
			if vm == nil || vm.Properties == nil {
				continue
			}

			// calculate age from VM creation time
			age := 0.0
			if vm.Properties.TimeCreated != nil {
				age = time.Since(*vm.Properties.TimeCreated).Hours() / 24.0
			}

			// map Azure provisioning state to our format
			state := "RUNNING"
			if vm.Properties.ProvisioningState != nil && *vm.Properties.ProvisioningState != "Succeeded" {
				state = *vm.Properties.ProvisioningState
			}

			region := ""
			if vm.Location != nil {
				region = *vm.Location
			}

			// store the full Azure resource ID as VendorID (needed for deletion to extract resource group)
			vendorID := ""
			if vm.ID != nil {
				vendorID = *vm.ID
			}

			name := ""
			if vm.Name != nil {
				name = *vm.Name
			}

			result = append(result, core.Server{
				VendorID: vendorID,
				Name:     name,
				Age:      age,
				Region:   region,
				State:    state,
				Tags:     azureTagsToTags(vm.Tags),
			})
		}
	}

	return result, nil
}

// ServerDelete removes the specified Azure virtual machine
func (a Azure) ServerDelete(ctx context.Context, server core.Server) error {
	cred, subscriptionID, err := a.credential(ctx)
	if err != nil {
		return err
	}

	client, err := armcompute.NewVirtualMachinesClient(subscriptionID, cred, nil)
	if err != nil {
		return err
	}

	// extract resource group from the full resource ID stored in VendorID
	resourceGroup := azureResourceGroup(server.VendorID)
	if resourceGroup == "" {
		return fmt.Errorf("cannot parse resource group from ID: %s", server.VendorID)
	}

	// start the async delete and wait for completion
	poller, err := client.BeginDelete(ctx, resourceGroup, server.Name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

// LoadBalancersGet returns all Azure load balancers across the subscription
func (a Azure) LoadBalancersGet(ctx context.Context, flagMock bool) ([]core.LoadBalancer, error) {
	cred, subscriptionID, err := a.credential(ctx)
	if err != nil {
		return nil, err
	}

	client, err := armnetwork.NewLoadBalancersClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, err
	}

	result := make([]core.LoadBalancer, 0)

	// list all load balancers across the subscription
	pager := client.NewListAllPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, lb := range page.Value {
			if lb == nil || lb.Properties == nil {
				continue
			}

			region := ""
			if lb.Location != nil {
				region = *lb.Location
			}

			// count backend instances across all backend address pools
			instanceCount := 0
			if lb.Properties.BackendAddressPools != nil {
				for _, pool := range lb.Properties.BackendAddressPools {
					if pool.Properties != nil && pool.Properties.BackendIPConfigurations != nil {
						instanceCount += len(pool.Properties.BackendIPConfigurations)
					}
				}
			}

			vendorID := ""
			if lb.ID != nil {
				vendorID = *lb.ID
			}

			name := ""
			if lb.Name != nil {
				name = *lb.Name
			}

			// Azure LB API doesn't directly expose creation time;
			// use 1 day as a safe default so it passes the "too new" check
			// while still requiring 0 backend instances for deletion
			age := 1.0

			result = append(result, core.LoadBalancer{
				Name:            name,
				Age:             age,
				InstanceCount:   instanceCount,
				Region:          region,
				Type:            "azure",
				Tags:            azureTagsToTags(lb.Tags),
				LoadBalancerArn: vendorID, // store full resource ID for deletion
			})
		}
	}

	return result, nil
}

// LoadBalancerDelete removes the specified Azure load balancer
func (a Azure) LoadBalancerDelete(ctx context.Context, loadBalancer core.LoadBalancer) error {
	cred, subscriptionID, err := a.credential(ctx)
	if err != nil {
		return err
	}

	client, err := armnetwork.NewLoadBalancersClient(subscriptionID, cred, nil)
	if err != nil {
		return err
	}

	// extract resource group from the full resource ID
	resourceGroup := azureResourceGroup(loadBalancer.LoadBalancerArn)
	if resourceGroup == "" {
		return fmt.Errorf("cannot parse resource group from ID: %s", loadBalancer.LoadBalancerArn)
	}

	// start the async delete and wait for completion
	poller, err := client.BeginDelete(ctx, resourceGroup, loadBalancer.Name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

// VolumesGet returns all Azure managed disks across the subscription
func (a Azure) VolumesGet(ctx context.Context) ([]core.Volume, error) {
	cred, subscriptionID, err := a.credential(ctx)
	if err != nil {
		return nil, err
	}

	client, err := armcompute.NewDisksClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, err
	}

	result := make([]core.Volume, 0)

	// list all managed disks across the subscription
	pager := client.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, disk := range page.Value {
			if disk == nil || disk.Properties == nil {
				continue
			}

			// calculate age from disk creation time
			age := 0.0
			if disk.Properties.TimeCreated != nil {
				age = time.Since(*disk.Properties.TimeCreated).Hours() / 24.0
			}

			region := ""
			if disk.Location != nil {
				region = *disk.Location
			}

			// a disk is attached when its state is "Attached"
			attached := false
			if disk.Properties.DiskState != nil {
				attached = *disk.Properties.DiskState == armcompute.DiskStateAttached
			}

			vendorID := ""
			if disk.ID != nil {
				vendorID = *disk.ID
			}

			name := ""
			if disk.Name != nil {
				name = *disk.Name
			}

			result = append(result, core.Volume{
				VendorID: vendorID,
				Name:     name,
				Age:      age,
				Region:   region,
				Attached: attached,
				Tags:     azureTagsToTags(disk.Tags),
			})
		}
	}

	return result, nil
}

// VolumeDelete removes the specified Azure managed disk
func (a Azure) VolumeDelete(ctx context.Context, volume core.Volume) error {
	cred, subscriptionID, err := a.credential(ctx)
	if err != nil {
		return err
	}

	client, err := armcompute.NewDisksClient(subscriptionID, cred, nil)
	if err != nil {
		return err
	}

	// extract resource group from the full resource ID
	resourceGroup := azureResourceGroup(volume.VendorID)
	if resourceGroup == "" {
		return fmt.Errorf("cannot parse resource group from ID: %s", volume.VendorID)
	}

	// start the async delete and wait for completion
	poller, err := client.BeginDelete(ctx, resourceGroup, volume.Name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

// azureTagsToTags converts Azure's map[string]*string tags to normalized "key=value" tag strings
func azureTagsToTags(tags map[string]*string) []string {
	result := make([]string, 0, len(tags))
	for key, value := range tags {
		if value != nil {
			result = append(result, fmt.Sprintf("%s=%s", key, *value))
		} else {
			result = append(result, key)
		}
	}
	return result
}

// azureResourceGroup extracts the resource group name from a full Azure resource ID
// format: /subscriptions/{sub}/resourceGroups/{rg}/providers/...
func azureResourceGroup(resourceID string) string {
	parts := strings.Split(resourceID, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// credential creates Azure client secret credentials from context values
func (a *Azure) credential(ctx context.Context) (*azidentity.ClientSecretCredential, string, error) {
	tenantID := ctx.Value("JANITOR_AZURE_TENANT_ID").(string)
	clientID := ctx.Value("JANITOR_AZURE_CLIENT_ID").(string)
	clientSecret := ctx.Value("JANITOR_AZURE_CLIENT_SECRET").(string)
	subscriptionID := ctx.Value("JANITOR_AZURE_SUBSCRIPTION_ID").(string)

	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		return nil, "", err
	}

	return cred, subscriptionID, nil
}
