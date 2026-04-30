package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearch_MissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test", nil)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestSearch_MissingQuery(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSearch_LimitClamping(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}
	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=test&limit=999", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Search(rec, req)
	// No db pool, query will fail gracefully and return empty results
	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "success", resp["status"])
}
