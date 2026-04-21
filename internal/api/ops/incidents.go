package ops

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

type incidentEvent struct {
	TS           string  `json:"ts"`
	AnomalyID    string  `json:"anomaly_id"`
	SimID        *string `json:"sim_id,omitempty"`
	Severity     string  `json:"severity"`
	Type         string  `json:"type"`
	Action       string  `json:"action"`
	ActorID      *string `json:"actor_id,omitempty"`
	ActorEmail   *string `json:"actor_email,omitempty"`
	Note         *string `json:"note,omitempty"`
	CurrentState string  `json:"current_state"`
}

type incidentFilters struct {
	from     *time.Time
	to       *time.Time
	severity string
	state    string
	entityID string
	cursor   string
	limit    int
}

func parseIncidentFilters(r *http.Request) (incidentFilters, error) {
	q := r.URL.Query()
	f := incidentFilters{limit: 50}

	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			f.limit = n
		}
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, fmt.Errorf("invalid 'from': %w", err)
		}
		f.from = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, fmt.Errorf("invalid 'to': %w", err)
		}
		f.to = &t
	}
	f.severity = q.Get("severity")
	f.state = q.Get("state")
	f.entityID = q.Get("entity_id")
	f.cursor = q.Get("cursor")
	return f, nil
}

func (h *Handler) Incidents(w http.ResponseWriter, r *http.Request) {
	isSuperAdmin := false
	if role, ok := r.Context().Value(apierr.RoleKey).(string); ok {
		isSuperAdmin = role == "super_admin"
	}

	var tenantID *uuid.UUID
	if tid, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID); ok && tid != uuid.Nil {
		tenantID = &tid
	}
	if !isSuperAdmin && tenantID == nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	filters, err := parseIncidentFilters(r)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, err.Error())
		return
	}
	if filters.severity != "" {
		if sevErr := severity.Validate(filters.severity); sevErr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidSeverity,
				"severity must be one of: critical, high, medium, low, info; got '"+filters.severity+"'")
			return
		}
	}

	events := h.buildIncidentTimeline(r, tenantID, filters)

	nextCursor := ""
	if len(events) > filters.limit {
		nextCursor = events[filters.limit-1].TS
		events = events[:filters.limit]
	}

	apierr.WriteList(w, http.StatusOK, events, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   filters.limit,
	})
}

func (h *Handler) buildIncidentTimeline(r *http.Request, tenantID *uuid.UUID, filters incidentFilters) []incidentEvent {
	ctx := r.Context()

	anomalyParams := store.ListAnomalyParams{
		Limit:    200,
		Severity: filters.severity,
		State:    filters.state,
		From:     filters.from,
		To:       filters.to,
	}
	if filters.entityID != "" {
		if id, err := uuid.Parse(filters.entityID); err == nil {
			anomalyParams.SimID = &id
		}
	}

	effectiveTenantID := uuid.Nil
	if tenantID != nil {
		effectiveTenantID = *tenantID
	}

	var events []incidentEvent

	if h.anomalyStore != nil && effectiveTenantID != uuid.Nil {
		anomalies, _, _ := h.anomalyStore.ListByTenant(ctx, effectiveTenantID, anomalyParams)
		for _, a := range anomalies {
			action := "detected"
			switch a.State {
			case "acknowledged":
				action = "acknowledged"
			case "resolved":
				action = "resolved"
			case "false_positive":
				action = "false_positive"
			}
			ev := incidentEvent{
				TS:           a.DetectedAt.Format(time.RFC3339),
				AnomalyID:    a.ID.String(),
				Severity:     a.Severity,
				Type:         a.Type,
				Action:       action,
				CurrentState: a.State,
			}
			if a.SimID != nil {
				s := a.SimID.String()
				ev.SimID = &s
			}
			events = append(events, ev)
		}
	}

	if h.auditStore != nil && effectiveTenantID != uuid.Nil {
		auditParams := store.ListAuditParams{
			Limit:      200,
			EntityType: "anomaly",
			From:       filters.from,
			To:         filters.to,
		}
		if filters.entityID != "" {
			auditParams.EntityID = filters.entityID
		}
		auditEntries, _, _ := h.auditStore.List(ctx, effectiveTenantID, auditParams)
		for _, e := range auditEntries {
			if !strings.HasPrefix(e.Action, "anomaly.") {
				continue
			}
			action := strings.TrimPrefix(e.Action, "anomaly.")
			ev := incidentEvent{
				TS:        e.CreatedAt.Format(time.RFC3339),
				AnomalyID: e.EntityID,
				Severity:  "",
				Type:      "",
				Action:    action,
			}
			if e.UserID != nil {
				s := e.UserID.String()
				ev.ActorID = &s
			}
			events = append(events, ev)
		}
	}

	sortIncidents(events)
	return events
}

// sortIncidents orders incident events strictly DESC by timestamp.
// Extracted as a package-level helper so unit tests can exercise the
// comparator directly without spinning up the full handler.
func sortIncidents(events []incidentEvent) {
	sort.Slice(events, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, events[i].TS)
		tj, _ := time.Parse(time.RFC3339, events[j].TS)
		return ti.After(tj)
	})
}
