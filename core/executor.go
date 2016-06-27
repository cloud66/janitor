package core

import "golang.org/x/net/context"

//Executor interface for cloud actions
type Executor interface {
	DeleteServer(ctx context.Context, server Server) error
	ListServers(ctx context.Context) ([]Server, error)
}
