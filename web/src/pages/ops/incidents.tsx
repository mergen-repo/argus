import { History, Filter, AlertTriangle, CheckCircle2, Bell } from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import { Select } from '@/components/ui/select'
import { useIncidents } from '@/hooks/use-ops'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { Incident } from '@/types/ops'
import { SeverityBadge } from '@/components/shared/severity-badge'
import { SEVERITY_FILTER_OPTIONS } from '@/lib/severity'

function ActionIcon({ action }: { action: string }) {
  switch (action) {
    case 'detected': return <Bell className="h-4 w-4 text-danger" />
    case 'acknowledged': return <AlertTriangle className="h-4 w-4 text-warning" />
    case 'resolved': return <CheckCircle2 className="h-4 w-4 text-success" />
    case 'escalated': return <AlertTriangle className="h-4 w-4 text-danger" />
    default: return <History className="h-4 w-4 text-text-tertiary" />
  }
}

export default function IncidentTimeline() {
  const [searchParams] = useSearchParams()
  const [severity, setSeverity] = useState(searchParams.get('severity') ?? '')
  const [state, setState] = useState(searchParams.get('state') ?? '')
  const entityId = searchParams.get('entity_id') ?? undefined

  const { data, isLoading } = useIncidents({
    severity: severity || undefined,
    state: state || undefined,
    entity_id: entityId,
    limit: 100,
  })

  const events: Incident[] = data?.data ?? []

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-96" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4 p-6 bg-bg-primary min-h-screen">
      <div className="flex items-center gap-2">
        <History className="h-4 w-4 text-accent" />
        <h1 className="text-[15px] font-semibold text-text-primary">Incident Timeline</h1>
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <Filter className="h-4 w-4 text-text-tertiary" />
        <Select
          value={severity}
          onChange={(e) => setSeverity(e.target.value)}
          options={SEVERITY_FILTER_OPTIONS}
          className="w-36 bg-bg-surface border-border text-text-primary text-[13px]"
        />
        <Select
          value={state}
          onChange={(e) => setState(e.target.value)}
          options={[
            { value: '', label: 'All states' },
            { value: 'open', label: 'Open' },
            { value: 'acknowledged', label: 'Acknowledged' },
            { value: 'resolved', label: 'Resolved' },
          ]}
          className="w-36 bg-bg-surface border-border text-text-primary text-[13px]"
        />
      </div>

      <Card className="bg-bg-surface border-border rounded-[10px] shadow-card">
        <CardHeader className="pb-3">
          <CardTitle className="text-[13px] font-medium text-text-secondary uppercase tracking-[1px]">
            Event Timeline ({events.length} events)
          </CardTitle>
        </CardHeader>
        <CardContent>
          {events.length === 0 ? (
            <div className="text-center py-8">
              <History className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
              <p className="text-[14px] text-text-secondary">No incidents found for selected filters.</p>
            </div>
          ) : (
            <div className="relative">
              <div className="absolute left-4 top-0 bottom-0 w-px bg-border" />
              <div className="space-y-1">
                {events.map((ev, i) => (
                  <div key={`${ev.anomaly_id}-${i}`} className="relative flex items-start gap-4 pl-10 py-2">
                    <div className="absolute left-2.5 top-3 h-3 w-3 rounded-full border-2 border-border bg-bg-surface flex items-center justify-center">
                      <div className={`h-1.5 w-1.5 rounded-full ${
                        ev.action === 'resolved' ? 'bg-success' :
                        ev.action === 'acknowledged' ? 'bg-warning' :
                        ev.action === 'escalated' ? 'bg-danger' : 'bg-accent'
                      }`} />
                    </div>

                    <div className="flex-1 rounded-[10px] border border-border bg-bg-elevated p-3">
                      <div className="flex items-center gap-2 flex-wrap">
                        <ActionIcon action={ev.action} />
                        <span className="text-[13px] font-medium text-text-primary capitalize">{ev.action}</span>
                        {ev.type && <span className="text-[12px] text-text-secondary">{ev.type}</span>}
                        {ev.severity && <SeverityBadge severity={ev.severity} />}
                        {ev.current_state && (
                          <Badge className="bg-bg-surface text-text-tertiary border border-border text-[10px]">{ev.current_state}</Badge>
                        )}
                        <span className="ml-auto text-[11px] font-mono text-text-tertiary">
                          {new Date(ev.ts).toLocaleString('tr-TR')}
                        </span>
                      </div>
                      {ev.actor_email && (
                        <div className="mt-1 text-[11px] text-text-tertiary">by {ev.actor_email}</div>
                      )}
                      {ev.note && (
                        <div className="mt-1 text-[12px] text-text-secondary italic">"{ev.note}"</div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
