package gateway

import (
	"os"

	"golang.org/x/sys/unix"
)

type DiskProbeResult struct {
	Mount   string  `json:"mount"`
	UsedPct float64 `json:"used_pct"`
	Status  string  `json:"status"`
}

func diskProbe(mounts []string, degradedPct, unhealthyPct int) []DiskProbeResult {
	results := make([]DiskProbeResult, 0, len(mounts))
	for _, mount := range mounts {
		if _, err := os.Stat(mount); os.IsNotExist(err) {
			results = append(results, DiskProbeResult{
				Mount:   mount,
				UsedPct: 0,
				Status:  "missing",
			})
			continue
		}

		var fs unix.Statfs_t
		if err := unix.Statfs(mount, &fs); err != nil {
			results = append(results, DiskProbeResult{
				Mount:   mount,
				UsedPct: 0,
				Status:  "missing",
			})
			continue
		}

		var usedPct float64
		if fs.Blocks > 0 {
			used := (fs.Blocks - fs.Bfree) * uint64(fs.Bsize)
			total := fs.Blocks * uint64(fs.Bsize)
			usedPct = float64(used) / float64(total) * 100.0
		}

		var status string
		switch {
		case int(usedPct) >= unhealthyPct:
			status = "unhealthy"
		case int(usedPct) >= degradedPct:
			status = "degraded"
		default:
			status = "ok"
		}

		results = append(results, DiskProbeResult{
			Mount:   mount,
			UsedPct: usedPct,
			Status:  status,
		})
	}
	return results
}
