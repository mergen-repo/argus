package export

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

func StreamCSV(w http.ResponseWriter, filename string, header []string, rows func(yield func([]string) bool)) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")

	cw := csv.NewWriter(w)

	if err := cw.Write(header); err != nil {
		return
	}

	flusher, canFlush := w.(http.Flusher)
	rowCount := 0

	rows(func(row []string) bool {
		if err := cw.Write(row); err != nil {
			return false
		}
		rowCount++
		if rowCount%500 == 0 {
			cw.Flush()
			if canFlush {
				flusher.Flush()
			}
		}
		return true
	})

	cw.Flush()
}

func BuildFilename(resource string, filters map[string]string) string {
	date := time.Now().UTC().Format("2006-01-02")

	if len(filters) == 0 {
		return fmt.Sprintf("%s_%s.csv", resource, date)
	}

	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := filters[k]
		if v == "" {
			continue
		}
		safe := strings.NewReplacer(" ", "-", "/", "-", ":", "-").Replace(v)
		parts = append(parts, k+"-"+safe)
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%s_%s.csv", resource, date)
	}

	return fmt.Sprintf("%s_%s_%s.csv", resource, strings.Join(parts, "_"), date)
}
