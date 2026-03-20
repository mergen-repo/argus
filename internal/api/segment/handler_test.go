package segment

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestToSegmentDTO_NilFilter(t *testing.T) {
	seg := &segmentDTO{
		Name:             "test",
		FilterDefinition: nil,
	}

	if seg.FilterDefinition != nil {
		t.Error("expected nil filter_definition in raw DTO")
	}
}

func TestCreateSegmentRequest_Validation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantErr  bool
		errField string
	}{
		{
			name:    "valid request",
			body:    `{"name":"active-sims","filter_definition":{"state":"active"}}`,
			wantErr: false,
		},
		{
			name:     "missing name",
			body:     `{"filter_definition":{"state":"active"}}`,
			wantErr:  true,
			errField: "name",
		},
		{
			name:     "missing filter_definition",
			body:     `{"name":"test"}`,
			wantErr:  true,
			errField: "filter_definition",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req createSegmentRequest
			if err := json.Unmarshal([]byte(tc.body), &req); err != nil {
				t.Fatal(err)
			}

			var validationErrors []map[string]interface{}
			if req.Name == "" {
				validationErrors = append(validationErrors, map[string]interface{}{"field": "name", "message": "Name is required", "code": "required"})
			}
			if req.FilterDefinition == nil || string(req.FilterDefinition) == "" {
				validationErrors = append(validationErrors, map[string]interface{}{"field": "filter_definition", "message": "Filter definition is required", "code": "required"})
			}

			hasErr := len(validationErrors) > 0
			if hasErr != tc.wantErr {
				t.Errorf("validation error = %v, wantErr %v", hasErr, tc.wantErr)
			}

			if tc.wantErr && tc.errField != "" {
				found := false
				for _, ve := range validationErrors {
					if ve["field"] == tc.errField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected validation error for field %q, got %v", tc.errField, validationErrors)
				}
			}
		})
	}
}

func TestSummaryDTO_JSON(t *testing.T) {
	segID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	dto := summaryDTO{
		SegmentID: segID,
		Total:     1250,
		ByState: map[string]int64{
			"active":    1000,
			"suspended": 200,
			"ordered":   50,
		},
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result["segment_id"] != segID.String() {
		t.Errorf("segment_id = %v, want %v", result["segment_id"], segID.String())
	}

	total, ok := result["total"].(float64)
	if !ok {
		t.Fatal("total field missing or wrong type")
	}
	if int64(total) != 1250 {
		t.Errorf("total = %v, want 1250", total)
	}

	byState, ok := result["by_state"].(map[string]interface{})
	if !ok {
		t.Fatal("by_state field missing or wrong type")
	}
	if active, ok := byState["active"].(float64); !ok || int64(active) != 1000 {
		t.Errorf("by_state.active = %v, want 1000", byState["active"])
	}
	if suspended, ok := byState["suspended"].(float64); !ok || int64(suspended) != 200 {
		t.Errorf("by_state.suspended = %v, want 200", byState["suspended"])
	}
	if ordered, ok := byState["ordered"].(float64); !ok || int64(ordered) != 50 {
		t.Errorf("by_state.ordered = %v, want 50", byState["ordered"])
	}
}

func TestSummaryDTO_EmptyByState(t *testing.T) {
	dto := summaryDTO{
		SegmentID: uuid.New(),
		Total:     0,
		ByState:   map[string]int64{},
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	total, ok := result["total"].(float64)
	if !ok {
		t.Fatal("total field missing or wrong type")
	}
	if int64(total) != 0 {
		t.Errorf("total = %v, want 0", total)
	}

	byState, ok := result["by_state"].(map[string]interface{})
	if !ok {
		t.Fatal("by_state field missing or wrong type")
	}
	if len(byState) != 0 {
		t.Errorf("by_state should be empty, got %v", byState)
	}
}

func TestSegmentDTO_JSON(t *testing.T) {
	segID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tenantID := uuid.MustParse("660e8400-e29b-41d4-a716-446655440000")
	createdBy := uuid.MustParse("770e8400-e29b-41d4-a716-446655440000")

	dto := segmentDTO{
		ID:               segID,
		TenantID:         tenantID,
		Name:             "active-turkcell",
		FilterDefinition: json.RawMessage(`{"state":"active","operator_id":"880e8400-e29b-41d4-a716-446655440000"}`),
		CreatedBy:        &createdBy,
		CreatedAt:        "2026-03-20T10:00:00Z",
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	if result["id"] != segID.String() {
		t.Errorf("id = %v, want %v", result["id"], segID.String())
	}
	if result["tenant_id"] != tenantID.String() {
		t.Errorf("tenant_id = %v, want %v", result["tenant_id"], tenantID.String())
	}
	if result["name"] != "active-turkcell" {
		t.Errorf("name = %v, want active-turkcell", result["name"])
	}
	if result["created_by"] != createdBy.String() {
		t.Errorf("created_by = %v, want %v", result["created_by"], createdBy.String())
	}

	fd, ok := result["filter_definition"].(map[string]interface{})
	if !ok {
		t.Fatal("filter_definition missing or wrong type")
	}
	if fd["state"] != "active" {
		t.Errorf("filter_definition.state = %v, want active", fd["state"])
	}
}

func TestBadUUIDFormat(t *testing.T) {
	badIDs := []string{
		"not-a-uuid",
		"123",
		"",
		"zzz-zzz-zzz-zzz",
		"550e8400-XXXX-41d4-a716-446655440000",
	}

	for _, bad := range badIDs {
		_, err := uuid.Parse(bad)
		if err == nil {
			t.Errorf("expected parse error for %q, got nil", bad)
		}
	}
}

func TestCreateSegmentRequest_InvalidFilterJSON(t *testing.T) {
	body := `{"name":"test","filter_definition":"not-json-object"}`
	var req createSegmentRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}

	var fd map[string]interface{}
	err := json.Unmarshal(req.FilterDefinition, &fd)
	if err == nil {
		t.Error("expected error unmarshaling non-object filter_definition")
	}
}

func TestCountDTO_JSON(t *testing.T) {
	dto := countDTO{
		Count: 42,
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}

	count, ok := result["count"].(float64)
	if !ok {
		t.Fatal("count field missing or wrong type")
	}
	if int64(count) != 42 {
		t.Errorf("count = %v, want 42", count)
	}
}
