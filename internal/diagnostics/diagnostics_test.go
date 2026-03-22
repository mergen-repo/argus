package diagnostics

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func TestCheckSIMState(t *testing.T) {
	svc := &Service{}

	tests := []struct {
		name       string
		state      string
		wantStatus string
	}{
		{"active", "active", StatusPass},
		{"suspended", "suspended", StatusFail},
		{"terminated", "terminated", StatusFail},
		{"stolen_lost", "stolen_lost", StatusFail},
		{"ordered", "ordered", StatusFail},
		{"unknown", "foo", StatusFail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim := &store.SIM{State: tt.state}
			result := svc.checkSIMState(sim)
			if result.Status != tt.wantStatus {
				t.Errorf("state=%q: got status=%q, want=%q", tt.state, result.Status, tt.wantStatus)
			}
			if result.Step != 1 {
				t.Errorf("step=%d, want=1", result.Step)
			}
			if result.Name != "SIM State" {
				t.Errorf("name=%q, want='SIM State'", result.Name)
			}
		})
	}
}

func TestCheckAPNConfig(t *testing.T) {
	svc := &Service{
		apnStore: nil,
	}

	t.Run("no APN assigned", func(t *testing.T) {
		sim := &store.SIM{APNID: nil}
		result := svc.checkAPNConfig(nil, sim)
		if result.Status != StatusFail {
			t.Errorf("got status=%q, want=%q", result.Status, StatusFail)
		}
	})
}

func TestCheckPolicy_NoVersion(t *testing.T) {
	svc := &Service{}

	sim := &store.SIM{PolicyVersionID: nil}
	result := svc.checkPolicy(nil, sim)
	if result.Status != StatusWarn {
		t.Errorf("got status=%q, want=%q", result.Status, StatusWarn)
	}
}

func TestCheckIPPool_NoAPN(t *testing.T) {
	svc := &Service{}

	sim := &store.SIM{APNID: nil}
	result := svc.checkIPPool(nil, sim)
	if result.Status != StatusWarn {
		t.Errorf("got status=%q, want=%q", result.Status, StatusWarn)
	}
}

func TestComputeOverall(t *testing.T) {
	tests := []struct {
		name  string
		steps []StepResult
		want  string
	}{
		{
			"all pass",
			[]StepResult{
				{Status: StatusPass},
				{Status: StatusPass},
				{Status: StatusPass},
			},
			OverallPass,
		},
		{
			"one warn",
			[]StepResult{
				{Status: StatusPass},
				{Status: StatusWarn},
				{Status: StatusPass},
			},
			OverallDegraded,
		},
		{
			"one fail",
			[]StepResult{
				{Status: StatusPass},
				{Status: StatusFail},
				{Status: StatusPass},
			},
			OverallFail,
		},
		{
			"fail and warn",
			[]StepResult{
				{Status: StatusWarn},
				{Status: StatusFail},
			},
			OverallFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeOverall(tt.steps)
			if got != tt.want {
				t.Errorf("got=%q, want=%q", got, tt.want)
			}
		})
	}
}

func TestIsThrottledToZero(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{"empty", "", false},
		{"no bandwidth", `{"foo":"bar"}`, false},
		{"max_bandwidth zero", `{"max_bandwidth":0}`, true},
		{"max_bandwidth nonzero", `{"max_bandwidth":1000}`, false},
		{"dl+ul zero", `{"download_rate":0,"upload_rate":0}`, true},
		{"dl+ul nonzero", `{"download_rate":100,"upload_rate":50}`, false},
		{"only dl zero", `{"download_rate":0}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.json != "" {
				raw = json.RawMessage(tt.json)
			}
			got := isThrottledToZero(raw)
			if got != tt.want {
				t.Errorf("got=%v, want=%v", got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{48 * time.Hour, "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("got=%q, want=%q", got, tt.want)
			}
		})
	}
}

func TestCheckOperatorHealth_NilStore(t *testing.T) {
	svc := &Service{
		operatorStore: nil,
	}

	sim := &store.SIM{OperatorID: uuid.New()}
	result := svc.checkOperatorHealth(nil, sim)
	if result.Step != 3 {
		t.Errorf("step=%d, want=3", result.Step)
	}
	if result.Status != StatusWarn {
		t.Errorf("got status=%q, want=%q", result.Status, StatusWarn)
	}
}

func TestCheckTestAuth(t *testing.T) {
	svc := &Service{}
	sim := &store.SIM{}
	result := svc.checkTestAuth(nil, sim)
	if result.Step != 7 {
		t.Errorf("step=%d, want=7", result.Step)
	}
	if result.Status != StatusWarn {
		t.Errorf("got status=%q, want=%q", result.Status, StatusWarn)
	}
}

func TestDiagnosticResultJSON(t *testing.T) {
	result := &DiagnosticResult{
		SimID:         uuid.New().String(),
		OverallStatus: OverallPass,
		Steps: []StepResult{
			{Step: 1, Name: "SIM State", Status: StatusPass, Message: "SIM is active"},
		},
		DiagnosedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded DiagnosticResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.OverallStatus != OverallPass {
		t.Errorf("overall=%q, want=%q", decoded.OverallStatus, OverallPass)
	}
	if len(decoded.Steps) != 1 {
		t.Fatalf("steps len=%d, want=1", len(decoded.Steps))
	}
	if decoded.Steps[0].Status != StatusPass {
		t.Errorf("step status=%q, want=%q", decoded.Steps[0].Status, StatusPass)
	}
}

func TestCheckLastAuth_NoSessionStore(t *testing.T) {
	svc := &Service{sessionStore: nil}
	sim := &store.SIM{ID: uuid.New()}
	result := svc.checkLastAuth(nil, sim)
	if result.Status != StatusWarn {
		t.Errorf("got status=%q, want=%q", result.Status, StatusWarn)
	}
}
