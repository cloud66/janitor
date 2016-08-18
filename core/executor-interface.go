package core

import "golang.org/x/net/context"

//ExecutorInterface base for cloud actions
type ExecutorInterface interface {
	ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]Server, error)
	ServerDelete(ctx context.Context, server Server) error
	ServerStop(ctx context.Context, server Server) error
	ServerStart(ctx context.Context, server Server) error
	LoadBalancersGet(ctx context.Context) ([]LoadBalancer, error)
	LoadBalancerDelete(ctx context.Context, loadBalancer LoadBalancer) error
	//GetSshKeys(ctx context.Context) ([]GetSshKeys, error) {}
	//DeleteSshKeys(ctx context.Context, sshKey SshKey) error {}

}
