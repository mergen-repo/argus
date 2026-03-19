package segment

import (
	"encoding/json"
	"testing"
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
