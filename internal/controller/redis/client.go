package redis

import (
	"context"

	redisv9 "github.com/redis/go-redis/v9"
)

type cmdableCustom func(ctx context.Context, cmd redisv9.Cmder) error

type CustomClient struct {
	*redisv9.Client
	cmdableCustom
}

func NewCustomClient(opt *redisv9.Options) *CustomClient {
	client := redisv9.NewClient(opt)
	customClient := &CustomClient{Client: client}
	customClient.cmdableCustom = client.Process
	return customClient
}

func (c cmdableCustom) ClusterSetslot(ctx context.Context, slot int, nodeId string, subCmd string) *redisv9.StatusCmd {
	// subCmd: IMPORTING, MIGRATING, NODE, STABLE
	cmd := redisv9.NewStatusCmd(ctx, "CLUSTER", "SETSLOT", slot, sub, nodeId)
	_ = c(ctx, cmd)
	return cmd
}
