package core

import (
	"context"
)

// Executor is a legacy no-op base retained only for backwards source-level
// compatibility. New executors should NOT embed this — instead they implement
// ExecutorInterface directly, which gives compile-time coverage.
type Executor struct{}

// Each method returns ErrUnsupported so callers can detect unsupported
// operations via errors.Is(err, ErrUnsupported).

func (e *Executor) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]Server, error) {
	return nil, ErrUnsupported
}

func (e *Executor) ServerDelete(ctx context.Context, server Server) error {
	return ErrUnsupported
}

func (e *Executor) ServerStop(ctx context.Context, server Server) error {
	return ErrUnsupported
}

func (e *Executor) ServerStart(ctx context.Context, server Server) error {
	return ErrUnsupported
}

func (e *Executor) LoadBalancersGet(ctx context.Context, flagMock bool) ([]LoadBalancer, error) {
	return nil, ErrUnsupported
}

func (e *Executor) LoadBalancerDelete(ctx context.Context, loadBalancer LoadBalancer) error {
	return ErrUnsupported
}

func (e *Executor) SshKeysGet(ctx context.Context) ([]SshKey, error) {
	return nil, ErrUnsupported
}

func (e *Executor) SshKeyDelete(ctx context.Context, sshKey SshKey) error {
	return ErrUnsupported
}

func (e *Executor) VolumesGet(ctx context.Context) ([]Volume, error) {
	return nil, ErrUnsupported
}

func (e *Executor) VolumeDelete(ctx context.Context, volume Volume) error {
	return ErrUnsupported
}
