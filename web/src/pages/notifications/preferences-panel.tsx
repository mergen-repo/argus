import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Save, Loader2 } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import {
  useNotificationPreferences,
  useUpsertNotificationPreferences,
  type NotificationPreference,
} from '@/hooks/use-notification-preferences'
import { useEventCatalog } from '@/hooks/use-event-catalog'
import { SEVERITY_OPTIONS as CANONICAL_SEVERITY_OPTIONS } from '@/lib/severity'

const CHANNELS = ['email', 'in_app', 'webhook', 'telegram', 'sms'] as const

const SEVERITY_OPTIONS = [...CANONICAL_SEVERITY_OPTIONS].reverse()

function emptyPref(eventType: string): NotificationPreference {
  return { event_type: eventType, channels: [], severity_threshold: 'info', enabled: true }
}

export function NotificationPreferencesPanel() {
  const query = useNotificationPreferences()
  const upsert = useUpsertNotificationPreferences()
  const { catalog, isLoading: catalogLoading } = useEventCatalog()
  const [matrix, setMatrix] = useState<Record<string, NotificationPreference>>({})
  const [dirty, setDirty] = useState(false)

  const tier1Set = useMemo(() => {
    if (!catalog) return new Set<string>()
    return new Set(catalog.filter((e) => e.tier === 'internal').map((e) => e.type))
  }, [catalog])

  const visibleEventTypes = useMemo(() => {
    if (!catalog) return [] as string[]
    return catalog.map((e) => e.type).filter((t) => !tier1Set.has(t))
  }, [catalog, tier1Set])

  useEffect(() => {
    if (!query.data) return
    const out: Record<string, NotificationPreference> = {}
    for (const evt of visibleEventTypes) {
      const existing = query.data.find((p) => p.event_type === evt)
      out[evt] = existing ?? emptyPref(evt)
    }
    setMatrix(out)
    setDirty(false)
  }, [query.data, visibleEventTypes])

  const update = (evt: string, patch: Partial<NotificationPreference>) => {
    setMatrix((prev) => ({ ...prev, [evt]: { ...prev[evt], ...patch } }))
    setDirty(true)
  }

  const toggleChannel = (evt: string, ch: string) => {
    setMatrix((prev) => {
      const has = prev[evt].channels.includes(ch)
      const channels = has ? prev[evt].channels.filter((c) => c !== ch) : [...prev[evt].channels, ch]
      return { ...prev, [evt]: { ...prev[evt], channels } }
    })
    setDirty(true)
  }

  const handleSave = async () => {
    try {
      await upsert.mutateAsync(Object.values(matrix))
      toast.success('Preferences saved')
      setDirty(false)
    } catch {
      toast.error('Failed to save preferences')
    }
  }

  return (
    <Card className="mt-3 p-4">
      <div className="flex items-center justify-between mb-3">
        <div>
          <h3 className="text-sm font-semibold text-text-primary">Per-Event Routing</h3>
          <p className="text-xs text-text-tertiary mt-0.5">Choose channels and severity threshold for each event type</p>
        </div>
        <Button onClick={handleSave} disabled={!dirty || upsert.isPending} size="sm" className="gap-1.5">
          {upsert.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
          Save
        </Button>
      </div>
      {query.isLoading || catalogLoading ? (
        <p className="text-sm text-text-tertiary">Loading preferences...</p>
      ) : (
        <>
          <p className="text-xs text-text-tertiary mb-2">
            Internal/metric events are not shown — they cannot be configured for notifications.
          </p>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Event</TableHead>
              {CHANNELS.map((c) => (
                <TableHead key={c} className="text-center text-[10px] uppercase">{c}</TableHead>
              ))}
              <TableHead>Severity ≥</TableHead>
              <TableHead>Enabled</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {visibleEventTypes.map((evt) => {
              const p = matrix[evt] ?? emptyPref(evt)
              return (
                <TableRow key={evt}>
                  <TableCell className="font-mono text-xs text-text-primary">{evt}</TableCell>
                  {CHANNELS.map((ch) => (
                    <TableCell key={ch} className="text-center">
                      <Checkbox
                        checked={p.channels.includes(ch)}
                        onChange={() => toggleChannel(evt, ch)}
                      />
                    </TableCell>
                  ))}
                  <TableCell>
                    <Select
                      value={p.severity_threshold}
                      onChange={(e) => update(evt, { severity_threshold: e.target.value })}
                      options={SEVERITY_OPTIONS}
                      className="h-7 text-xs"
                    />
                  </TableCell>
                  <TableCell>
                    <Checkbox
                      checked={p.enabled}
                      onChange={(e) => update(evt, { enabled: e.target.checked })}
                    />
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        </>
      )}
    </Card>
  )
}
