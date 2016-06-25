package core

import "golang.org/x/net/context"

type Executor interface {
	DeleteServer(ctx context.Context, server Server) error
	ListServers(ctx context.Context) ([]Server, error)
}
