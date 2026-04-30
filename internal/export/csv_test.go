package export_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/export"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamCSV_OutputFormat(t *testing.T) {
	w := httptest.NewRecorder()
	export.StreamCSV(w, "test.csv", []string{"id", "name"}, func(yield func([]string) bool) {
		for i := 0; i < 3; i++ {
			if !yield([]string{"id-1", "name-1"}) {
				return
			}
		}
	})

	resp := w.Result()
	assert.Equal(t, "text/csv; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Contains(t, resp.Header.Get("Content-Disposition"), "test.csv")
	body := w.Body.String()
	lines := strings.Split(strings.TrimSpace(body), "\n")
	assert.Equal(t, 4, len(lines)) // header + 3 rows
	assert.Equal(t, "id,name", lines[0])
}

func TestStreamCSV_10kRows(t *testing.T) {
	w := httptest.NewRecorder()
	export.StreamCSV(w, "large.csv", []string{"id"}, func(yield func([]string) bool) {
		for i := 0; i < 10000; i++ {
			if !yield([]string{"row"}) {
				return
			}
		}
	})
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	assert.Equal(t, 10001, len(lines)) // header + 10k rows
}

func TestBuildFilename(t *testing.T) {
	name := export.BuildFilename("sims", map[string]string{"state": "active", "operator": "vodafone"})
	require.Contains(t, name, "sims_")
	require.Contains(t, name, "operator-vodafone")
	require.Contains(t, name, "state-active")
	require.True(t, strings.HasSuffix(name, ".csv"))
}

func TestBuildFilename_NoFilters(t *testing.T) {
	name := export.BuildFilename("apns", nil)
	assert.True(t, strings.HasPrefix(name, "apns_"))
	assert.True(t, strings.HasSuffix(name, ".csv"))
}
