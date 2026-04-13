package ops

import (
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	dto "github.com/prometheus/client_model/go"
)

// Snapshot is recomputed at most every snapshotCacheTTL to keep
// /ops/metrics/snapshot cheap when polled by the dashboard (15s default
// refresh on the FE).
const snapshotCacheTTL = 5 * time.Second

var (
	snapshotCacheMu sync.Mutex
	snapshotCachedAt time.Time
	snapshotCached  snapshotResponse
)

type routeMetric struct {
	Method     string  `json:"method"`
	Route      string  `json:"route"`
	Count      float64 `json:"count"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	P99Ms      float64 `json:"p99_ms"`
	ErrorCount float64 `json:"error_count"`
	ErrorRate  float64 `json:"error_rate"`
}

type byStatus struct {
	Status string  `json:"status"`
	Count  float64 `json:"count"`
}

type httpBlock struct {
	Totals  httpTotals   `json:"totals"`
	ByRoute []routeMetric `json:"by_route"`
	ByStatus []byStatus  `json:"by_status"`
}

type httpTotals struct {
	Requests  float64 `json:"requests"`
	Errors    float64 `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
}

type aaaProtocol struct {
	Protocol   string  `json:"protocol"`
	ReqPerSec  float64 `json:"req_per_sec"`
	SuccessRate float64 `json:"success_rate"`
	P99Ms      float64 `json:"p99_ms"`
}

type aaaBlock struct {
	ByProtocol []aaaProtocol `json:"by_protocol"`
}

type runtimeBlock struct {
	Goroutines    int     `json:"goroutines"`
	MemAllocBytes uint64  `json:"mem_alloc_bytes"`
	MemSysBytes   uint64  `json:"mem_sys_bytes"`
	GCPauseP99Ms  float64 `json:"gc_pause_p99_ms"`
}

type jobType struct {
	JobType string  `json:"job_type"`
	Runs    float64 `json:"runs"`
	Success float64 `json:"success"`
	Failed  float64 `json:"failed"`
	P50S    float64 `json:"p50_s"`
	P95S    float64 `json:"p95_s"`
	P99S    float64 `json:"p99_s"`
}

type jobsBlock struct {
	ByType []jobType `json:"by_type"`
}

type snapshotResponse struct {
	HTTP    httpBlock    `json:"http"`
	AAA     aaaBlock     `json:"aaa"`
	Runtime runtimeBlock `json:"runtime"`
	Jobs    jobsBlock    `json:"jobs"`
}

func histogramPercentiles(h *dto.Histogram, pctls ...float64) []float64 {
	if h == nil {
		result := make([]float64, len(pctls))
		return result
	}
	buckets := h.GetBucket()
	totalCount := float64(h.GetSampleCount())
	if totalCount == 0 {
		return make([]float64, len(pctls))
	}

	result := make([]float64, len(pctls))
	for i, p := range pctls {
		target := p * totalCount
		var prevUpperBound float64
		var prevCount float64
		found := false
		for _, b := range buckets {
			upper := b.GetUpperBound()
			count := float64(b.GetCumulativeCount())
			if upper >= 1e15 {
				break
			}
			if count >= target {
				if count == prevCount {
					result[i] = upper * 1000
				} else {
					frac := (target - prevCount) / (count - prevCount)
					result[i] = (prevUpperBound + frac*(upper-prevUpperBound)) * 1000
				}
				found = true
				break
			}
			prevUpperBound = upper
			prevCount = count
		}
		if !found {
			result[i] = prevUpperBound * 1000
		}
	}
	return result
}

func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	if h.metricsReg == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Metrics registry not configured")
		return
	}

	// Serve from cache when within TTL — Prometheus.Gather + histogram
	// percentile aggregation across thousands of label series is not free,
	// so cap recompute frequency to snapshotCacheTTL.
	snapshotCacheMu.Lock()
	if !snapshotCachedAt.IsZero() && time.Since(snapshotCachedAt) < snapshotCacheTTL {
		cached := snapshotCached
		snapshotCacheMu.Unlock()
		apierr.WriteSuccess(w, http.StatusOK, cached)
		return
	}
	snapshotCacheMu.Unlock()

	families, err := h.metricsReg.Reg.Gather()
	if err != nil {
		h.logger.Error().Err(err).Msg("gather prometheus metrics")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to gather metrics")
		return
	}

	familyMap := make(map[string]*dto.MetricFamily, len(families))
	for _, f := range families {
		familyMap[f.GetName()] = f
	}

	resp := snapshotResponse{}

	httpTotalsMap := map[string]float64{}
	routeMap := map[string]*routeMetric{}
	statusMap := map[string]float64{}

	if f := familyMap["argus_http_requests_total"]; f != nil {
		for _, m := range f.GetMetric() {
			labels := labelsToMap(m.GetLabel())
			method := labels["method"]
			route := labels["route"]
			status := labels["status"]
			count := m.GetCounter().GetValue()

			key := method + ":" + route
			if _, ok := routeMap[key]; !ok {
				routeMap[key] = &routeMetric{Method: method, Route: route}
			}
			routeMap[key].Count += count

			is5xx := len(status) > 0 && status[0] == '5'
			is4xx := len(status) > 0 && status[0] == '4'
			if is4xx || is5xx {
				routeMap[key].ErrorCount += count
				httpTotalsMap["errors"] += count
			}
			httpTotalsMap["total"] += count
			statusMap[status] += count
		}
	}

	if f := familyMap["argus_http_request_duration_seconds"]; f != nil {
		durationMap := map[string]*dto.Histogram{}
		for _, m := range f.GetMetric() {
			labels := labelsToMap(m.GetLabel())
			method := labels["method"]
			route := labels["route"]
			key := method + ":" + route
			durationMap[key] = m.GetHistogram()
		}
		for key, rm := range routeMap {
			if hist, ok := durationMap[key]; ok {
				ps := histogramPercentiles(hist, 0.5, 0.95, 0.99)
				rm.P50Ms = ps[0]
				rm.P95Ms = ps[1]
				rm.P99Ms = ps[2]
			}
		}
	}

	routes := make([]routeMetric, 0, len(routeMap))
	for _, rm := range routeMap {
		if rm.Count > 0 {
			rm.ErrorRate = rm.ErrorCount / rm.Count
		}
		routes = append(routes, *rm)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Count > routes[j].Count })

	statusList := make([]byStatus, 0, len(statusMap))
	for s, c := range statusMap {
		statusList = append(statusList, byStatus{Status: s, Count: c})
	}
	sort.Slice(statusList, func(i, j int) bool { return statusList[i].Status < statusList[j].Status })

	total := httpTotalsMap["total"]
	errors := httpTotalsMap["errors"]
	var errRate float64
	if total > 0 {
		errRate = errors / total
	}
	resp.HTTP = httpBlock{
		Totals:   httpTotals{Requests: total, Errors: errors, ErrorRate: errRate},
		ByRoute:  routes,
		ByStatus: statusList,
	}

	aaaProtocolMap := map[string]*aaaProtocol{}
	if f := familyMap["argus_aaa_auth_requests_total"]; f != nil {
		for _, m := range f.GetMetric() {
			labels := labelsToMap(m.GetLabel())
			proto := labels["protocol"]
			result := labels["result"]
			count := m.GetCounter().GetValue()
			if _, ok := aaaProtocolMap[proto]; !ok {
				aaaProtocolMap[proto] = &aaaProtocol{Protocol: proto}
			}
			aaaProtocolMap[proto].ReqPerSec += count
			if result == "success" || result == "accept" {
				aaaProtocolMap[proto].SuccessRate += count
			}
		}
	}
	if f := familyMap["argus_aaa_auth_latency_seconds"]; f != nil {
		for _, m := range f.GetMetric() {
			labels := labelsToMap(m.GetLabel())
			proto := labels["protocol"]
			ps := histogramPercentiles(m.GetHistogram(), 0.99)
			if ap, ok := aaaProtocolMap[proto]; ok {
				ap.P99Ms = ps[0]
			}
		}
	}
	for _, ap := range aaaProtocolMap {
		total := ap.ReqPerSec
		if total > 0 {
			ap.SuccessRate = ap.SuccessRate / total
		}
		resp.AAA.ByProtocol = append(resp.AAA.ByProtocol, *ap)
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	gcPauseP99 := float64(0)
	var maxPause uint64
	for _, p := range memStats.PauseNs {
		if p > maxPause {
			maxPause = p
		}
	}
	gcPauseP99 = float64(maxPause) / 1e6
	resp.Runtime = runtimeBlock{
		Goroutines:    runtime.NumGoroutine(),
		MemAllocBytes: memStats.Alloc,
		MemSysBytes:   memStats.Sys,
		GCPauseP99Ms:  gcPauseP99,
	}

	jobMap := map[string]*jobType{}
	if f := familyMap["argus_job_runs_total"]; f != nil {
		for _, m := range f.GetMetric() {
			labels := labelsToMap(m.GetLabel())
			jt := labels["job_type"]
			result := labels["result"]
			count := m.GetCounter().GetValue()
			if _, ok := jobMap[jt]; !ok {
				jobMap[jt] = &jobType{JobType: jt}
			}
			jobMap[jt].Runs += count
			if result == "success" {
				jobMap[jt].Success += count
			} else if strings.Contains(result, "fail") || result == "error" {
				jobMap[jt].Failed += count
			}
		}
	}
	if f := familyMap["argus_job_duration_seconds"]; f != nil {
		for _, m := range f.GetMetric() {
			labels := labelsToMap(m.GetLabel())
			jt := labels["job_type"]
			ps := histogramPercentiles(m.GetHistogram(), 0.5, 0.95, 0.99)
			if j, ok := jobMap[jt]; ok {
				j.P50S = ps[0] / 1000
				j.P95S = ps[1] / 1000
				j.P99S = ps[2] / 1000
			}
		}
	}
	for _, jt := range jobMap {
		resp.Jobs.ByType = append(resp.Jobs.ByType, *jt)
	}

	snapshotCacheMu.Lock()
	snapshotCached = resp
	snapshotCachedAt = time.Now()
	snapshotCacheMu.Unlock()

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func labelsToMap(labels []*dto.LabelPair) map[string]string {
	m := make(map[string]string, len(labels))
	for _, l := range labels {
		m[l.GetName()] = l.GetValue()
	}
	return m
}
