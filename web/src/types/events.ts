// FIX-212 — canonical NATS/WS envelope types. Mirrors internal/bus/envelope.go.
// This file is type-only (no runtime code). Do NOT mount or import from a
// page in FIX-212 — FIX-240 is the downstream story that wires the
// notification-preferences UI to GET /api/v1/events/catalog.

export type EventSeverity = 'critical' | 'high' | 'medium' | 'low' | 'info';

export interface EntityRef {
  type: string;
  id: string;
  display_name?: string;
}

export interface BusEnvelope<M = Record<string, unknown>> {
  event_version: number;
  id: string;
  type: string;
  timestamp: string;
  tenant_id: string;
  severity: EventSeverity;
  source: string;
  title: string;
  message?: string;
  entity?: EntityRef;
  dedup_key?: string;
  meta?: M;
}

export interface EventCatalogEntry {
  type: string;
  source: string;
  default_severity: EventSeverity;
  entity_type: string;
  description: string;
  meta_schema: Record<string, string>;
}

export interface EventCatalogResponse {
  status: 'success';
  data: {
    events: EventCatalogEntry[];
  };
}

// Helper: extract a stable display label for a BusEnvelope row. Falls back
// to entity.id when display_name is absent (legacy shape or resolver miss).
export function envelopeRowLabel<M>(env: BusEnvelope<M>): string {
  if (env.entity) {
    return env.entity.display_name || env.entity.id;
  }
  return env.title;
}
