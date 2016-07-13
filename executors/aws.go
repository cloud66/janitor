package executors

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/cloud66/janitor/core"
	"golang.org/x/net/context"
)

//Aws encapsulates all AWS cloud calls
type Aws struct {
}

//ListServers return all servers in account
func (a Aws) ListServers(context context.Context) ([]core.Server, error) {
	accessKey := context.Value("AWS_ACCESS_KEY_ID").(string)
	secretKey := context.Value("AWS_SECRET_ACCESS_KEY").(string)
	session := session.New(aws.NewConfig().WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, "")))

	result := make([]core.Server, 0, 0)
	regions := []string{"ap-northeast-1", "ap-northeast-2", "ap-southeast-1", "ap-southeast-2", "eu-west-1", "sa-east-1", "us-east-1", "us-west-1", "us-west-2", "eu-central-1"}

	for _, region := range regions {
		svc := ec2.New(session, &aws.Config{Region: aws.String(region)})

		resp, err := svc.DescribeInstances(nil)
		if err != nil {
			panic(err)
		}
		// resp has all of the response data, pull out instance IDs:
		for idx := range resp.Reservations {
			// spew.Dump(res)
			// fmt.Println("  > Number of instances: ", len(res.Instances))
			for _, inst := range resp.Reservations[idx].Instances {
				// spew.Dump(inst)
				age := time.Now().Sub(*inst.LaunchTime).Hours() / 24.0
				name := *inst.InstanceId
				for _, tag := range inst.Tags {
					if *tag.Key == "Name" {
						name = *tag.Value
					}
				}
				result = append(result, core.Server{VendorID: *inst.InstanceId, Name: name, Age: age, Region: region})
			}
		}
	}

	return result, nil
}

// DeleteServer remove the specified server
func (a Aws) DeleteServer(context context.Context, server core.Server) error {
	accessKey := context.Value("AWS_ACCESS_KEY_ID").(string)
	secretKey := context.Value("AWS_SECRET_ACCESS_KEY").(string)
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
