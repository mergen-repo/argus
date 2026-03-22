package bench

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	radius "layeh.com/radius"
	"layeh.com/radius/rfc2865"
)

type LoadGenConfig struct {
	TargetAddr   string
	Secret       string
	Concurrency  int
	RatePerSec   int
	Duration     time.Duration
	IMSIPrefix   string
	IMSICount    int
	NASIPPrefix  string
}

func DefaultLoadGenConfig() LoadGenConfig {
	return LoadGenConfig{
		TargetAddr:  "127.0.0.1:1812",
		Secret:      "testing123",
		Concurrency: 100,
		RatePerSec:  10000,
		Duration:    60 * time.Second,
		IMSIPrefix:  "28601",
		IMSICount:   100000,
		NASIPPrefix: "10.0.0.",
	}
}

type LoadGenResult struct {
	TotalSent     int64
	TotalSuccess  int64
	TotalFailed   int64
	TotalTimeout  int64
	Duration      time.Duration
	RateActual    float64
	LatencyP50    time.Duration
	LatencyP95    time.Duration
	LatencyP99    time.Duration
	LatencyMin    time.Duration
	LatencyMax    time.Duration
	LatencyAvg    time.Duration
}

func (r *LoadGenResult) String() string {
	return fmt.Sprintf(
		"Sent=%d Success=%d Failed=%d Timeout=%d Duration=%s Rate=%.0f/s P50=%s P95=%s P99=%s Min=%s Max=%s Avg=%s",
		r.TotalSent, r.TotalSuccess, r.TotalFailed, r.TotalTimeout,
		r.Duration.Truncate(time.Millisecond),
		r.RateActual,
		r.LatencyP50, r.LatencyP95, r.LatencyP99,
		r.LatencyMin, r.LatencyMax, r.LatencyAvg,
	)
}

type LoadGenerator struct {
	cfg      LoadGenConfig
	imsis    []string

	sent    atomic.Int64
	success atomic.Int64
	failed  atomic.Int64
	timeout atomic.Int64

	latencies []time.Duration
	latMu     sync.Mutex
}

func NewLoadGenerator(cfg LoadGenConfig) *LoadGenerator {
	imsis := make([]string, cfg.IMSICount)
	for i := 0; i < cfg.IMSICount; i++ {
		imsis[i] = fmt.Sprintf("%s%010d", cfg.IMSIPrefix, i)
	}
	return &LoadGenerator{
		cfg:       cfg,
		imsis:     imsis,
		latencies: make([]time.Duration, 0, cfg.RatePerSec*int(cfg.Duration.Seconds())),
	}
}

func (lg *LoadGenerator) Run(ctx context.Context) *LoadGenResult {
	ctx, cancel := context.WithTimeout(ctx, lg.cfg.Duration)
	defer cancel()

	interval := time.Second / time.Duration(lg.cfg.RatePerSec)
	if interval <= 0 {
		interval = time.Microsecond
	}

	sem := make(chan struct{}, lg.cfg.Concurrency)

	var wg sync.WaitGroup
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			elapsed := time.Since(start)
			return lg.buildResult(elapsed)
		case <-ticker.C:
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer func() {
					<-sem
					wg.Done()
				}()
				lg.sendAuth(ctx)
			}()
		}
	}
}

func (lg *LoadGenerator) sendAuth(ctx context.Context) {
	lg.sent.Add(1)

	imsi := lg.imsis[rand.Intn(len(lg.imsis))]
	secret := []byte(lg.cfg.Secret)

	pkt := radius.New(radius.CodeAccessRequest, secret)
	rfc2865.UserName_SetString(pkt, imsi)
	nasIP := fmt.Sprintf("%s%d", lg.cfg.NASIPPrefix, rand.Intn(254)+1)
	rfc2865.NASIPAddress_Set(pkt, net.ParseIP(nasIP).To4())

	start := time.Now()
	resp, err := radius.Exchange(ctx, pkt, lg.cfg.TargetAddr)
	elapsed := time.Since(start)

	if err != nil {
		if ctx.Err() != nil {
			lg.timeout.Add(1)
		} else {
			lg.failed.Add(1)
		}
		return
	}

	if resp.Code == radius.CodeAccessAccept || resp.Code == radius.CodeAccessReject {
		lg.success.Add(1)
	} else {
		lg.failed.Add(1)
	}

	lg.latMu.Lock()
	lg.latencies = append(lg.latencies, elapsed)
	lg.latMu.Unlock()
}

func (lg *LoadGenerator) buildResult(elapsed time.Duration) *LoadGenResult {
	result := &LoadGenResult{
		TotalSent:    lg.sent.Load(),
		TotalSuccess: lg.success.Load(),
		TotalFailed:  lg.failed.Load(),
		TotalTimeout: lg.timeout.Load(),
		Duration:     elapsed,
	}

	if elapsed > 0 {
		result.RateActual = float64(result.TotalSent) / elapsed.Seconds()
	}

	lg.latMu.Lock()
	defer lg.latMu.Unlock()

	if len(lg.latencies) == 0 {
		return result
	}

	sortDurations(lg.latencies)

	n := len(lg.latencies)
	result.LatencyP50 = lg.latencies[n*50/100]
	result.LatencyP95 = lg.latencies[n*95/100]

	p99Idx := n * 99 / 100
	if p99Idx >= n {
		p99Idx = n - 1
	}
	result.LatencyP99 = lg.latencies[p99Idx]
	result.LatencyMin = lg.latencies[0]
	result.LatencyMax = lg.latencies[n-1]

	var total time.Duration
	for _, d := range lg.latencies {
		total += d
	}
	result.LatencyAvg = total / time.Duration(n)

	return result
}

func sortDurations(ds []time.Duration) {
	n := len(ds)
	for i := 1; i < n; i++ {
		key := ds[i]
		j := i - 1
		for j >= 0 && ds[j] > key {
			ds[j+1] = ds[j]
			j--
		}
		ds[j+1] = key
	}
}
