package ops

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
)

func TestHistogramPercentiles_Basic(t *testing.T) {
	ub1 := float64(0.01)
	ub2 := float64(0.1)
	ub3 := float64(1.0)
	ub4 := float64(10.0)

	c1 := uint64(50)
	c2 := uint64(90)
	c3 := uint64(99)
	c4 := uint64(100)

	sc := uint64(100)
	ss := float64(1.5)

	h := &dto.Histogram{
		SampleCount: &sc,
		SampleSum:   &ss,
		Bucket: []*dto.Bucket{
			{UpperBound: &ub1, CumulativeCount: &c1},
			{UpperBound: &ub2, CumulativeCount: &c2},
			{UpperBound: &ub3, CumulativeCount: &c3},
			{UpperBound: &ub4, CumulativeCount: &c4},
		},
	}

	ps := histogramPercentiles(h, 0.5, 0.95, 0.99)
	if ps[0] <= 0 {
		t.Errorf("p50 should be > 0, got %f", ps[0])
	}
	if ps[1] <= ps[0] {
		t.Errorf("p95 (%f) should be > p50 (%f)", ps[1], ps[0])
	}
	if ps[2] <= ps[1] {
		t.Errorf("p99 (%f) should be > p95 (%f)", ps[2], ps[1])
	}
}

func TestHistogramPercentiles_NilHistogram(t *testing.T) {
	ps := histogramPercentiles(nil, 0.5, 0.95, 0.99)
	if len(ps) != 3 {
		t.Errorf("expected 3 results, got %d", len(ps))
	}
	for _, p := range ps {
		if p != 0 {
			t.Errorf("nil histogram should return 0 percentiles, got %f", p)
		}
	}
}

func TestHistogramPercentiles_EmptyHistogram(t *testing.T) {
	sc := uint64(0)
	ss := float64(0)
	h := &dto.Histogram{
		SampleCount: &sc,
		SampleSum:   &ss,
	}
	ps := histogramPercentiles(h, 0.5)
	if ps[0] != 0 {
		t.Errorf("empty histogram should return 0, got %f", ps[0])
	}
}

func TestParseRedisInfo_Basic(t *testing.T) {
	raw := `# Memory
used_memory:52428800
maxmemory:1073741824

# Stats
keyspace_hits:1000
keyspace_misses:100
instantaneous_ops_per_sec:4000
evicted_keys:0

# Clients
connected_clients:18

# Keyspace
db0:keys=12345,expires=11000
`
	info := parseRedisInfo(raw)
	if info["used_memory"] != "52428800" {
		t.Errorf("expected used_memory=52428800, got %q", info["used_memory"])
	}
	if info["maxmemory"] != "1073741824" {
		t.Errorf("expected maxmemory=1073741824, got %q", info["maxmemory"])
	}
	if info["connected_clients"] != "18" {
		t.Errorf("expected connected_clients=18, got %q", info["connected_clients"])
	}
	if info["db0"] != "keys=12345,expires=11000" {
		t.Errorf("expected db0 keyspace, got %q", info["db0"])
	}
}

func TestParseRedisInfo_EmptyLines(t *testing.T) {
	raw := `

# Section header

key1:val1

key2:val2
`
	info := parseRedisInfo(raw)
	if info["key1"] != "val1" {
		t.Errorf("expected key1=val1, got %q", info["key1"])
	}
	if info["key2"] != "val2" {
		t.Errorf("expected key2=val2, got %q", info["key2"])
	}
	if _, ok := info["# Section header"]; ok {
		t.Error("should not include comment lines")
	}
}

func TestSnapshot_NoRegistry(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.Snapshot(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestInfraHealth_NoPool(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.InfraHealth(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (partial success), got %d", w.Code)
	}
}

func TestLabelsToMap(t *testing.T) {
	name1 := "method"
	val1 := "GET"
	name2 := "route"
	val2 := "/api/v1/sims"
	labels := []*dto.LabelPair{
		{Name: &name1, Value: &val1},
		{Name: &name2, Value: &val2},
	}
	m := labelsToMap(labels)
	if m["method"] != "GET" {
		t.Errorf("expected method=GET, got %q", m["method"])
	}
	if m["route"] != "/api/v1/sims" {
		t.Errorf("expected route=/api/v1/sims, got %q", m["route"])
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"42", 42},
		{"3.14", 3.14},
		{"0", 0},
		{"", 0},
		{"1234567890", 1234567890},
	}
	for _, tt := range tests {
		got := parseFloat(tt.input)
		if got != tt.expected {
			t.Errorf("parseFloat(%q): got %f, want %f", tt.input, got, tt.expected)
		}
	}
}
