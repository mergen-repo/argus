package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type WSBroadcaster interface {
	BroadcastAll(eventType string, data interface{})
}

type Pusher struct {
	collector *Collector
	hub       WSBroadcaster
	logger    zerolog.Logger
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func NewPusher(collector *Collector, hub WSBroadcaster, logger zerolog.Logger) *Pusher {
	return &Pusher{
		collector: collector,
		hub:       hub,
		logger:    logger.With().Str("component", "metrics_pusher").Logger(),
		stopCh:    make(chan struct{}),
	}
}

func (p *Pusher) Start() {
	p.wg.Add(1)
	go p.run()
	p.logger.Info().Msg("metrics pusher started (1s interval)")
}

func (p *Pusher) Stop() {
	close(p.stopCh)
	p.wg.Wait()
	p.logger.Info().Msg("metrics pusher stopped")
}

func (p *Pusher) run() {
	defer p.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.push()
		}
	}
}

func (p *Pusher) push() {
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()

	m, err := p.collector.GetMetrics(ctx)
	if err != nil {
		p.logger.Warn().Err(err).Msg("failed to collect metrics for push")
		return
	}

	payload := ToRealtimePayload(m)
	p.hub.BroadcastAll("metrics.realtime", payload)
}
