package metrics

import "time"

func SetRecent5xxNow(r *Registry, now func() time.Time) {
	if r == nil || r.recent5xx == nil {
		return
	}
	r.recent5xx.now = now
}
