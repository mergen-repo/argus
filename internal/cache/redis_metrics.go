package cache

import (
	"context"
	"net"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/redis/go-redis/v9"
)

type metricsHook struct {
	reg *metrics.Registry
}

func NewMetricsHook(reg *metrics.Registry) redis.Hook {
	return &metricsHook{reg: reg}
}

func (h *metricsHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := next(ctx, network, addr)
		if h.reg != nil {
			h.reg.RedisOpsTotal.WithLabelValues("dial", resultLabel(err)).Inc()
		}
		return conn, err
	}
}

func (h *metricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if h.reg != nil {
			h.reg.RedisOpsTotal.WithLabelValues(cmd.Name(), resultLabel(err)).Inc()
		}
		return err
	}
}

func (h *metricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		err := next(ctx, cmds)
		if h.reg != nil {
			h.reg.RedisOpsTotal.WithLabelValues("pipeline", resultLabel(err)).Inc()
		}
		return err
	}
}

func resultLabel(err error) string {
	if err == nil || err == redis.Nil {
		return "success"
	}
	return "error"
}

func RegisterRedisMetrics(client *redis.Client, reg *metrics.Registry) {
	if client == nil || reg == nil {
		return
	}
	client.AddHook(NewMetricsHook(reg))
}
