package executors

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/cloud66/janitor/core"
	"time"
)

//Aws encapsulates all AWS cloud calls
type Aws struct {
	*core.Executor
}

//ServersGet return all servers in account
func (a Aws) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	results := make([]core.Server, 0, 0)
	if regions == nil {
		// get from here: https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Concepts.RegionsAndAvailabilityZones.html
		regions = a.allRegions()
	}
	for _, region := range regions {
		ec2Client := a.ec2Client(ctx, region)
		describeInstancesOutput, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
		if err != nil {
			continue
		}
		//resp has the response data, pull out instance IDs:
		for _, reservation := range describeInstancesOutput.Reservations {
			// fmt.Println(resp)
			for _, instance := range reservation.Instances {
				vendorID := *instance.InstanceId
				if vendorIDs != nil {
					found := false
					for _, desiredVendor := range vendorIDs {
						if vendorID == desiredVendor {
							found = true
							break
						}
					}
					if !found {
						//not one of our desired ids
						continue
					}
				}

				if len(instance.BlockDeviceMappings) == 0 {
					continue
				}

				attachTime := *instance.BlockDeviceMappings[0].Ebs.AttachTime
				age := time.Now().Sub(attachTime).Hours() / 24.0
				name := vendorID
				for _, tag := range instance.Tags {
					if *tag.Key == "Name" {
						name = *tag.Value
					}
				}

				if instance.State.Name != "terminated" && instance.State.Name != "shutting-down" {
					state := "RUNNING"
					results = append(results, core.Server{VendorID: vendorID, Name: name, Age: age, Region: region, State: state})
				}
			}
		}
	}
	return results, nil
}

//LoadBalancersGet return all load balancers in account
func (a Aws) LoadBalancersGet(ctx context.Context) ([]core.LoadBalancer, error) {
	results := make([]core.LoadBalancer, 0, 0)
	for _, region := range a.allRegions() {
		elbClient := a.elbClient(ctx, region)
		elbOutput, err := elbClient.DescribeLoadBalancers(ctx, &elasticloadbalancing.DescribeLoadBalancersInput{})
		if err == nil {
			for idx := range elbOutput.LoadBalancerDescriptions {
				loadBalancer := elbOutput.LoadBalancerDescriptions[idx]
				age := time.Now().Sub(*loadBalancer.CreatedTime).Hours() / 24.0
				name := loadBalancer.LoadBalancerName
				var vendorIDs []string
				for _, instance := range loadBalancer.Instances {
					vendorIDs = append(vendorIDs, *instance.InstanceId)
				}
				servers, _ := a.ServersGet(ctx, vendorIDs, []string{region})
				instanceCount := 0
				for _, server := range servers {
					if server.State != "TERMINATED" {
						instanceCount = instanceCount + 1
					}
				}
				results = append(results, core.LoadBalancer{Name: *name, Age: age, InstanceCount: instanceCount, Region: region, Type: "elb"})
			}
		}
		// elastic load balancing v2
		albClient := a.albClient(ctx, region)
		albOutput, err := albClient.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
		if err == nil {
			for idx := range albOutput.LoadBalancers {
				loadBalancer := albOutput.LoadBalancers[idx]
				age := time.Now().Sub(*loadBalancer.CreatedTime).Hours() / 24.0
				name := loadBalancer.LoadBalancerName
				loadBalancerArn := loadBalancer.LoadBalancerArn

				tagsOutput, err := albClient.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{ResourceArns: []string{*loadBalancerArn}})
				if err == nil {
					for _, tagDescription := range tagsOutput.TagDescriptions {
						for _, tag := range tagDescription.Tags {
							if *tag.Key == "C66-STACK" {
								name = tag.Value
							}
						}
					}
				}

				var listenerArns []string
				listenerOutput, err := albClient.DescribeListeners(ctx, &elasticloadbalancingv2.DescribeListenersInput{LoadBalancerArn: loadBalancerArn})
				if err == nil {
					for _, listener := range listenerOutput.Listeners {
						listenerArns = append(listenerArns, *listener.ListenerArn)
					}
				}
				var targetGroupArns []string
				targetGroupOutput, err := albClient.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{LoadBalancerArn: loadBalancerArn})
				if err == nil {
					for _, targetGroup := range targetGroupOutput.TargetGroups {
						targetGroupArns = append(targetGroupArns, *targetGroup.TargetGroupArn)
					}
				}
				instanceCount := 999
				results = append(results, core.LoadBalancer{
					Name:            *name,
					Age:             age,
					InstanceCount:   instanceCount,
					Region:          region,
					Type:            "alb",
					LoadBalancerArn: *loadBalancerArn,
					ListenerArns:    listenerArns,
					TargetGroupArns: targetGroupArns,
				})
			}
		}
	}
	return results, nil
}

//ServerDelete remove the specified server
func (a Aws) ServerDelete(ctx context.Context, server core.Server) error {
	ec2Client := a.ec2Client(ctx, server.Region)
	_, err := ec2Client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId:            aws.String(server.VendorID),
		DisableApiTermination: &types.AttributeBooleanValue{Value: aws.Bool(false)},
		DryRun:                aws.Bool(false),
	})
	if err != nil {
		return err
	}
	_, err = ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{server.VendorID},
		DryRun:      aws.Bool(false),
	})
	if err != nil {
		return err
	}
	return nil
}

//LoadBalancerDelete delete the load balancer
func (a Aws) LoadBalancerDelete(ctx context.Context, loadBalancer core.LoadBalancer) error {
	if loadBalancer.Type == "elb" {
		elbClient := a.elbClient(ctx, loadBalancer.Region)
		_, err := elbClient.DeleteLoadBalancer(ctx, &elasticloadbalancing.DeleteLoadBalancerInput{
			LoadBalancerName: aws.String(loadBalancer.Name),
		})
		if err != nil {
			return err
		}
		return nil
	} else if loadBalancer.Type == "alb" {
		albClient := a.albClient(ctx, loadBalancer.Region)
		for _, listenerArn := range loadBalancer.ListenerArns {
			_, err := albClient.DeleteListener(ctx, &elasticloadbalancingv2.DeleteListenerInput{ListenerArn: &listenerArn})
			if err != nil {
				return err
			}
		}
		for _, targetGroupArn := range loadBalancer.TargetGroupArns {
			_, err := albClient.DeleteTargetGroup(ctx, &elasticloadbalancingv2.DeleteTargetGroupInput{TargetGroupArn: &targetGroupArn})
			if err != nil {
				return err
			}
		}
		_, err := albClient.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{LoadBalancerArn: &loadBalancer.LoadBalancerArn})
		if err != nil {
			return err
		}
		return nil
	}
	return errors.New("unrecognised LB type")
}

func (a Aws) ec2Client(ctx context.Context, region string) *ec2.Client {
	return ec2.New(ec2.Options{
		Region:      region,
		Credentials: a.credentials(ctx),
	})
}

func (a Aws) elbClient(ctx context.Context, region string) *elasticloadbalancing.Client {
	return elasticloadbalancing.New(elasticloadbalancing.Options{
		Region:      region,
		Credentials: a.credentials(ctx),
	})
}

func (a Aws) albClient(ctx context.Context, region string) *elasticloadbalancingv2.Client {
	return elasticloadbalancingv2.New(elasticloadbalancingv2.Options{
		Region:      region,
		Credentials: a.credentials(ctx),
	})
}

var credentialsCache *aws.CredentialsCache

func (a Aws) credentials(ctx context.Context) *aws.CredentialsCache {
	if credentialsCache != nil {
		return credentialsCache
	}
	accessKey := ctx.Value("JANITOR_AWS_ACCESS_KEY_ID").(string)
	secretKey := ctx.Value("JANITOR_AWS_SECRET_ACCESS_KEY").(string)
	credentialsProvider := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")
	credentialsCache = aws.NewCredentialsCache(credentialsProvider)
	return credentialsCache
}

func (a Aws) allRegions() []string {
	return []string{"af-south-1", "ap-east-1", "ap-northeast-1", "ap-northeast-2", "ap-northeast-3", "ap-south-1", "ap-southeast-1", "ap-southeast-2", "ap-southeast-3", "ca-central-1", "eu-central-1", "eu-north-1", "eu-south-1", "eu-west-1", "eu-west-2", "eu-west-3", "me-central-1", "me-south-1", "sa-east-1", "us-east-1", "us-east-2", "us-gov-east-1", "us-gov-west-1", "us-west-1", "us-west-2"}
}
