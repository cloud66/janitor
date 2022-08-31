package core

import (
	"errors"

	"golang.org/x/net/context"
)

//Executor base for cloud actions
type Executor struct{}

//ServersGet gets servers
func (e *Executor) ServersGet(context context.Context, vendorIDs []string, regions []string) ([]Server, error) {
	return nil, errors.New("action not available")
}

//ServerDelete deletes a server
func (e *Executor) ServerDelete(ctx context.Context, server Server) error {
	return errors.New("action not available")
}

//ServerStop stops a server
func (e *Executor) ServerStop(ctx context.Context, server Server) error {
	return errors.New("action not available")
}

//ServerStart starts a server
func (e *Executor) ServerStart(ctx context.Context, server Server) error {
	return errors.New("action not available")
}

//LoadBalancersGet gets load balancers
func (e *Executor) LoadBalancersGet(ctx context.Context) ([]LoadBalancer, error) {
	return nil, errors.New("action not available")
}

//LoadBalancerDelete deletes a load balancer
func (e *Executor) LoadBalancerDelete(ctx context.Context, loadBalancer LoadBalancer) error {
	return errors.New("action not available")
}

//SshKeysGet gets SSH keys
func (e *Executor) SshKeysGet(ctx context.Context) ([]SshKey, error) {
	return nil, errors.New("action not available")
}

//SshKeyDelete deletes an SSH key
func (e *Executor) SshKeyDelete(ctx context.Context, sshKey SshKey) error {
	return errors.New("action not available")
}
