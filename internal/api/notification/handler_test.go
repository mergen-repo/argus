package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestHandler_List_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_List_NoUserContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_MarkRead_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/"+uuid.New().String()+"/read", nil)
	w := httptest.NewRecorder()

	h.MarkRead(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_MarkRead_InvalidID(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "invalid-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notifications/invalid-uuid/read", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.MarkRead(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_MarkAllRead_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read-all", nil)
	w := httptest.NewRecorder()

	h.MarkAllRead(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_MarkAllRead_NoUserContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/read-all", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.MarkAllRead(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_GetConfigs_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-configs", nil)
	w := httptest.NewRecorder()

	h.GetConfigs(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_UpdateConfigs_NoTenantContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_UpdateConfigs_InvalidBody(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(`invalid`))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_UpdateConfigs_EmptyConfigs(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(`{"configs":[]}`))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_UpdateConfigs_InvalidEventType(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)

	body := `{"configs":[{"event_type":"invalid.event","scope_type":"system","channels":{"email":true},"enabled":true}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_UpdateConfigs_InvalidScopeType(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)

	body := `{"configs":[{"event_type":"operator.down","scope_type":"invalid","channels":{"email":true},"enabled":true}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-configs", strings.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdateConfigs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandler_GetConfigs_NoUserContext(t *testing.T) {
	h := NewHandler(nil, nil, nil, zerolog.Nop())

	tenantID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-configs", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetConfigs(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

type mockPrefStore struct {
	matrix    []*store.NotificationPreference
	upsertErr error
}

func (m *mockPrefStore) GetMatrix(_ context.Context, _ uuid.UUID) ([]*store.NotificationPreference, error) {
	if m.matrix == nil {
		return []*store.NotificationPreference{}, nil
	}
	return m.matrix, nil
}

func (m *mockPrefStore) Upsert(_ context.Context, _ uuid.UUID, prefs []store.NotificationPreference) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.matrix = make([]*store.NotificationPreference, 0, len(prefs))
	for i := range prefs {
		p := prefs[i]
		m.matrix = append(m.matrix, &p)
	}
	return nil
}

func newHandlerWithPrefStore(ps *mockPrefStore) *Handler {
	h := NewHandler(nil, nil, nil, zerolog.Nop())
	h.prefStore = ps
	return h
}

func ctxWithTenant(tenantID uuid.UUID) context.Context {
	return context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
}

func TestHandler_GetPreferences_EmptyMatrix(t *testing.T) {
	h := newHandlerWithPrefStore(&mockPrefStore{})

	ctx := ctxWithTenant(uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-preferences", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	h.GetPreferences(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Status string          `json:"status"`
		Data   []preferenceDTO `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
	if len(resp.Data) != 0 {
		t.Errorf("data len = %d, want 0", len(resp.Data))
	}
}

func TestHandler_GetPreferences_NoTenantContext(t *testing.T) {
	h := newHandlerWithPrefStore(&mockPrefStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-preferences", nil)
	w := httptest.NewRecorder()

	h.GetPreferences(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_UpdatePreferences_Upserts(t *testing.T) {
	ps := &mockPrefStore{}
	h := newHandlerWithPrefStore(ps)

	ctx := ctxWithTenant(uuid.New())
	body := `[{"event_type":"operator.down","channels":["email","in_app"],"severity_threshold":"warning","enabled":true}]`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-preferences", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdatePreferences(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if len(ps.matrix) != 1 {
		t.Fatalf("matrix len = %d, want 1", len(ps.matrix))
	}
	if ps.matrix[0].EventType != "operator.down" {
		t.Errorf("event_type = %q, want operator.down", ps.matrix[0].EventType)
	}
}

func TestHandler_UpdatePreferences_InvalidChannel(t *testing.T) {
	h := newHandlerWithPrefStore(&mockPrefStore{})

	ctx := ctxWithTenant(uuid.New())
	body := `[{"event_type":"operator.down","channels":["fax"],"severity_threshold":"info","enabled":true}]`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-preferences", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdatePreferences(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_UpdatePreferences_InvalidSeverity(t *testing.T) {
	h := newHandlerWithPrefStore(&mockPrefStore{})

	ctx := ctxWithTenant(uuid.New())
	body := `[{"event_type":"operator.down","channels":["email"],"severity_threshold":"urgent","enabled":true}]`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-preferences", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdatePreferences(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_UpdatePreferences_EmptyEventType(t *testing.T) {
	h := newHandlerWithPrefStore(&mockPrefStore{})

	ctx := ctxWithTenant(uuid.New())
	body := `[{"event_type":"","channels":["email"],"severity_threshold":"info","enabled":true}]`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-preferences", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpdatePreferences(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

type mockTemplateStore struct {
	templates map[string]*store.NotificationTemplate
}

func (m *mockTemplateStore) List(_ context.Context, eventType, locale string) ([]*store.NotificationTemplate, error) {
	var results []*store.NotificationTemplate
	for _, t := range m.templates {
		if (eventType == "" || t.EventType == eventType) && (locale == "" || t.Locale == locale) {
			tc := *t
			results = append(results, &tc)
		}
	}
	if results == nil {
		results = []*store.NotificationTemplate{}
	}
	return results, nil
}

func (m *mockTemplateStore) Upsert(_ context.Context, t *store.NotificationTemplate) error {
	if m.templates == nil {
		m.templates = make(map[string]*store.NotificationTemplate)
	}
	key := t.EventType + "/" + t.Locale
	tc := *t
	tc.UpdatedAt = time.Now()
	m.templates[key] = &tc
	return nil
}

func (m *mockTemplateStore) Get(_ context.Context, eventType, locale string) (*store.NotificationTemplate, error) {
	if m.templates == nil {
		return nil, errors.New("store: notification template not found")
	}
	key := eventType + "/" + locale
	if t, ok := m.templates[key]; ok {
		return t, nil
	}
	enKey := eventType + "/en"
	if t, ok := m.templates[enKey]; ok {
		return t, nil
	}
	return nil, errors.New("store: notification template not found")
}

func newHandlerWithTemplateStore(ts *mockTemplateStore) *Handler {
	h := NewHandler(nil, nil, nil, zerolog.Nop())
	h.templateStore = ts
	return h
}

func TestHandler_ListTemplates_NoTenantContext(t *testing.T) {
	h := newHandlerWithTemplateStore(&mockTemplateStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-templates", nil)
	w := httptest.NewRecorder()

	h.ListTemplates(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandler_ListTemplates_ReturnsAll(t *testing.T) {
	ts := &mockTemplateStore{
		templates: map[string]*store.NotificationTemplate{
			"operator.down/en": {EventType: "operator.down", Locale: "en", Subject: "Operator Down", BodyText: "Operator is down"},
			"operator.down/tr": {EventType: "operator.down", Locale: "tr", Subject: "Operatör Çevrimdışı", BodyText: "Operatör çevrimdışı"},
		},
	}
	h := newHandlerWithTemplateStore(ts)

	ctx := ctxWithTenant(uuid.New())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-templates", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListTemplates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Status string        `json:"status"`
		Data   []templateDTO `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Errorf("data len = %d, want 2", len(resp.Data))
	}
}

func TestHandler_UpsertTemplate_InvalidLocale(t *testing.T) {
	h := newHandlerWithTemplateStore(&mockTemplateStore{})

	ctx := ctxWithTenant(uuid.New())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("event_type", "operator.down")
	rctx.URLParams.Add("locale", "fr")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	body := `{"subject":"Subj","body_text":"Body"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-templates/operator.down/fr", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpsertTemplate(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_UpsertTemplate_EmptySubject(t *testing.T) {
	h := newHandlerWithTemplateStore(&mockTemplateStore{})

	ctx := ctxWithTenant(uuid.New())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("event_type", "operator.down")
	rctx.URLParams.Add("locale", "en")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	body := `{"subject":"","body_text":"Body"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-templates/operator.down/en", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpsertTemplate(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_UpsertTemplate_Success(t *testing.T) {
	ts := &mockTemplateStore{}
	h := newHandlerWithTemplateStore(ts)

	ctx := ctxWithTenant(uuid.New())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("event_type", "operator.down")
	rctx.URLParams.Add("locale", "tr")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	body := `{"subject":"Operatör Çevrimdışı","body_text":"Operatör çevrimdışı oldu"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/notification-templates/operator.down/tr", strings.NewReader(body)).WithContext(ctx)
	w := httptest.NewRecorder()

	h.UpsertTemplate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Status string      `json:"status"`
		Data   templateDTO `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Subject != "Operatör Çevrimdışı" {
		t.Errorf("subject = %q, want Operatör Çevrimdışı", resp.Data.Subject)
	}
}
