// Canonical severity taxonomy — mirrors Go internal/severity/severity.go (FIX-211).

export const SEVERITY_VALUES = ['critical', 'high', 'medium', 'low', 'info'] as const;
export type Severity = typeof SEVERITY_VALUES[number];

export const SEVERITY_OPTIONS: ReadonlyArray<{ value: Severity; label: string }> = [
  { value: 'critical', label: 'Critical' },
  { value: 'high',     label: 'High' },
  { value: 'medium',   label: 'Medium' },
  { value: 'low',      label: 'Low' },
  { value: 'info',     label: 'Info' },
];

export const SEVERITY_FILTER_OPTIONS: ReadonlyArray<{ value: '' | Severity; label: string }> = [
  { value: '',         label: 'All Severities' },
  ...SEVERITY_OPTIONS,
];

const ORDINAL_MAP: Record<Severity, number> = {
  info: 1, low: 2, medium: 3, high: 4, critical: 5,
};

export function severityOrdinal(s: string): number {
  return (ORDINAL_MAP as Record<string, number>)[s] ?? 0;
}

export function isSeverity(s: string): s is Severity {
  return (SEVERITY_VALUES as readonly string[]).includes(s);
}

export function severityLabel(s: string): string {
  if (!isSeverity(s)) return s;
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// Foreground text-class for icons rendered adjacent to severity-tinted content.
// Mirrors the textClass values in SeverityBadge's SEVERITY_CONFIG to prevent
// drift across sites that render an icon only (no full pill).
export const SEVERITY_ICON_CLASS: Record<Severity, string> = {
  critical: 'text-danger',
  high:     'text-danger',
  medium:   'text-warning',
  low:      'text-info',
  info:     'text-text-secondary',
};

export function severityIconClass(s: string): string {
  return isSeverity(s) ? SEVERITY_ICON_CLASS[s] : 'text-text-secondary';
}

// Pill (selected-state) background + foreground token pair, for filter UIs that
// highlight the active severity. Mirrors SEVERITY_CONFIG tokens; exported so
// callers stop re-declaring the map inline.
export const SEVERITY_PILL_CLASSES: Record<Severity, string> = {
  critical: 'bg-danger-dim text-danger',
  high:     'bg-danger-dim text-danger',
  medium:   'bg-warning-dim text-warning',
  low:      'bg-info/10 text-info',
  info:     'bg-bg-elevated text-text-secondary',
};
