import { AlertOctagon, AlertTriangle, Info } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { isSeverity, severityLabel, SEVERITY_ICON_CLASS, type Severity } from '@/lib/severity';

export interface SeverityBadgeProps {
  severity: string;
  className?: string;
  iconOnly?: boolean;
  label?: string;
}

type SeverityConfig = {
  icon: React.ElementType;
  bgClass: string;
  textClass: string;
  extraClass?: string;
};

// textClass values here MUST match SEVERITY_ICON_CLASS in web/src/lib/severity.ts.
// If you change one, change the other (single-source drift prevention).
const SEVERITY_CONFIG: Record<Severity, SeverityConfig> = {
  critical: { icon: AlertOctagon,  bgClass: 'bg-danger-dim',    textClass: SEVERITY_ICON_CLASS.critical, extraClass: 'animate-pulse' },
  high:     { icon: AlertTriangle, bgClass: 'bg-danger-dim',    textClass: SEVERITY_ICON_CLASS.high },
  medium:   { icon: AlertTriangle, bgClass: 'bg-warning-dim',   textClass: SEVERITY_ICON_CLASS.medium },
  low:      { icon: Info,          bgClass: 'bg-info/10',        textClass: SEVERITY_ICON_CLASS.low },
  info:     { icon: Info,          bgClass: 'bg-bg-elevated',   textClass: SEVERITY_ICON_CLASS.info },
};

const FALLBACK_CONFIG: SeverityConfig = {
  icon: Info,
  bgClass: 'bg-bg-elevated',
  textClass: 'text-text-secondary',
};

export function SeverityBadge({ severity, className, iconOnly, label }: SeverityBadgeProps) {
  const config = isSeverity(severity) ? SEVERITY_CONFIG[severity] : FALLBACK_CONFIG;
  const { icon: Icon, bgClass, textClass, extraClass } = config;
  const displayLabel = label ?? severityLabel(severity);

  if (iconOnly) {
    return (
      <span className={cn(textClass, className)}>
        <Icon className="h-4 w-4" />
      </span>
    );
  }

  return (
    <Badge
      variant="outline"
      className={cn(
        'border-0 gap-1 text-[10px] uppercase tracking-wide',
        bgClass,
        textClass,
        extraClass,
        className,
      )}
    >
      <Icon className="h-3 w-3" />
      {displayLabel}
    </Badge>
  );
}
