package executors

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/cloud66/janitor/core"
	"golang.org/x/net/context"
)

//Aws encapsulates all AWS cloud calls
type Aws struct {
	*core.Executor
}

//ServersGet return all servers in account
func (a Aws) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	accessKey := ctx.Value("JANITOR_AWS_ACCESS_KEY_ID").(string)
	secretKey := ctx.Value("JANITOR_AWS_SECRET_ACCESS_KEY").(string)
	session := session.New(aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, "")))

	results := make([]core.Server, 0, 0)
	if regions == nil {
		regions = []string{"ap-northeast-1", "ap-northeast-2", "ap-southeast-1", "ap-southeast-2", "eu-west-1", "sa-east-1", "us-east-1", "us-west-1", "us-west-2", "eu-central-1"}
	}
	for _, region := range regions {
		svc := ec2.New(session, &aws.Config{Region: aws.String(region)})

		resp, err := svc.DescribeInstances(nil)
		if err != nil {
			panic(err)
		}
		//resp has all of the response data, pull out instance IDs:
		for _, reservation := range resp.Reservations {
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

				attachTime := *instance.BlockDeviceMappings[0].Ebs.AttachTime
				age := time.Now().Sub(attachTime).Hours() / 24.0
				name := vendorID
				for _, tag := range instance.Tags {
					if *tag.Key == "Name" {
						name = *tag.Value
					}
				}

				if *instance.State.Name != "terminated" && *instance.State.Name != "shutting-down" {
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
	accessKey := ctx.Value("JANITOR_AWS_ACCESS_KEY_ID").(string)
	secretKey := ctx.Value("JANITOR_AWS_SECRET_ACCESS_KEY").(string)
	session := session.New(aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, "")))

	results := make([]core.LoadBalancer, 0, 0)
	regions := []string{"ap-northeast-1", "ap-northeast-2", "ap-southeast-1", "ap-southeast-2", "eu-west-1", "sa-east-1", "us-east-1", "us-west-1", "us-west-2", "eu-central-1"}

	for _, region := range regions {
		svc := elb.New(session, &aws.Config{Region: aws.String(region)})
		resp, err := svc.DescribeLoadBalancers(nil)
		if err != nil {
			panic(err)
		}
		// fmt.Println(resp)
		for idx := range resp.LoadBalancerDescriptions {
			loadBalancer := resp.LoadBalancerDescriptions[idx]
			age := time.Now().Sub(*loadBalancer.CreatedTime).Hours() / 24.0
			name := loadBalancer.LoadBalancerName
			vendorIDs := []string{}
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
			results = append(results, core.LoadBalancer{Name: *name, Age: age, InstanceCount: instanceCount, Region: region})
		}
	}
	return results, nil
}

//ServerDelete remove the specified server
func (a Aws) ServerDelete(ctx context.Context, server core.Server) error {
	accessKey := ctx.Value("JANITOR_AWS_ACCESS_KEY_ID").(string)
	secretKey := ctx.Value("JANITOR_AWS_SECRET_ACCESS_KEY").(string)
	session := session.New(aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, "")))
	svc := ec2.New(session, &aws.Config{Region: aws.String(server.Region)})
	params := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(server.VendorID),
		},
		DryRun: aws.Bool(false),
	}
	_, err := svc.TerminateInstances(params)
	if err != nil {
		return err
	}
	return nil
}

//ServerStop remove the specified server
func (a Aws) ServerStop(ctx context.Context, server core.Server) error {
	accessKey := ctx.Value("JANITOR_AWS_ACCESS_KEY_ID").(string)
	secretKey := ctx.Value("JANITOR_AWS_SECRET_ACCESS_KEY").(string)
	session := session.New(aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, "")))
	svc := ec2.New(session, &aws.Config{Region: aws.String(server.Region)})
	params := &ec2.StopInstancesInput{
		InstanceIds: []*string{
			aws.String(server.VendorID),
		},
		DryRun: aws.Bool(false),
	}
	_, err := svc.StopInstances(params)
	if err != nil {
		return err
	}
	return nil
}

//ServerStart start the specified server
func (a Aws) ServerStart(ctx context.Context, server core.Server) error {
	accessKey := ctx.Value("JANITOR_AWS_ACCESS_KEY_ID").(string)
	secretKey := ctx.Value("JANITOR_AWS_SECRET_ACCESS_KEY").(string)
	session := session.New(aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, "")))
	svc := ec2.New(session, &aws.Config{Region: aws.String(server.Region)})
	params := &ec2.StartInstancesInput{
		InstanceIds: []*string{
			aws.String(server.VendorID),
		},
		DryRun: aws.Bool(false),
	}
	_, err := svc.StartInstances(params)
	if err != nil {
		return err
	}
	return nil
}

//LoadBalancerDelete delete the load balancer
func (a Aws) LoadBalancerDelete(ctx context.Context, loadBalancer core.LoadBalancer) error {
	accessKey := ctx.Value("JANITOR_AWS_ACCESS_KEY_ID").(string)
	secretKey := ctx.Value("JANITOR_AWS_SECRET_ACCESS_KEY").(string)
	session := session.New(aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, "")))
	svc := elb.New(session, &aws.Config{Region: aws.String(loadBalancer.Region)})
	params := &elb.DeleteLoadBalancerInput{
		LoadBalancerName: aws.String(loadBalancer.Name),
	}
	_, err := svc.DeleteLoadBalancer(params)
	if err != nil {
		return err
	}
	return nil
}
