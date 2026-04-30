// Tab body for /settings#notifications. Channel config + Simple/Advanced preference views (FIX-240 AC-2).
import { useState, useEffect, useMemo } from 'react'
import { Save, Loader2, AlertCircle } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { useNotificationConfig, useUpdateNotificationConfig } from '@/hooks/use-settings'
import {
  useNotificationPreferences,
  useUpsertNotificationPreferences,
  type NotificationPreference,
} from '@/hooks/use-notification-preferences'
import { useEventCatalog } from '@/hooks/use-event-catalog'
import { NotificationPreferencesPanel } from '@/pages/notifications/preferences-panel'
import {
  ChannelConfigSection,
  DEFAULT_CHANNEL_CONFIG,
  validateWebhookUrl,
  validateWebhookSecret,
} from './notifications-channel-config'
import type { NotificationConfig } from '@/types/settings'

// ─── Simple View ──────────────────────────────────────────────────────────────

function emptyPref(eventType: string): NotificationPreference {
  return { event_type: eventType, channels: [], severity_threshold: 'info', enabled: true }
}

interface SimpleViewProps {
  onDirty: () => void
  matrixRef: { current: Record<string, NotificationPreference> }
}

function NotificationPreferencesSimple({ onDirty, matrixRef }: SimpleViewProps) {
  const { catalog, isLoading: catalogLoading, error: catalogError } = useEventCatalog()
  const prefsQuery = useNotificationPreferences()
  const [matrix, setMatrix] = useState<Record<string, NotificationPreference>>({})

  const visibleCatalog = useMemo(() => {
    if (!catalog) return []
    return catalog.filter((e) => e.tier !== 'internal')
  }, [catalog])

  const bySource = useMemo(() => {
    const groups: Record<string, typeof visibleCatalog> = {}
    for (const entry of visibleCatalog) {
      const key = entry.source || 'other'
      if (!groups[key]) groups[key] = []
      groups[key].push(entry)
    }
    return groups
  }, [visibleCatalog])

  useEffect(() => {
    if (!prefsQuery.data) return
    const out: Record<string, NotificationPreference> = {}
    for (const entry of visibleCatalog) {
      const existing = prefsQuery.data.find((p) => p.event_type === entry.type)
      out[entry.type] = existing ?? emptyPref(entry.type)
    }
    setMatrix(out)
    matrixRef.current = out
  }, [prefsQuery.data, visibleCatalog, matrixRef])

  const isCategoryChecked = (events: typeof visibleCatalog) =>
    events.every((e) => matrix[e.type]?.enabled !== false)

  const toggleCategory = (events: typeof visibleCatalog, checked: boolean) => {
    setMatrix((prev) => {
      const next = { ...prev }
      for (const e of events) {
        next[e.type] = { ...(next[e.type] ?? emptyPref(e.type)), enabled: checked }
      }
      matrixRef.current = next
      return next
    })
    onDirty()
  }

  if (catalogLoading || prefsQuery.isLoading) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
      </div>
    )
  }

  if (catalogError || prefsQuery.isError) {
    return (
      <div className="flex items-center gap-2 rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2">
        <AlertCircle className="h-3.5 w-3.5 text-danger shrink-0" />
        <span className="text-xs text-danger">Failed to load event catalog</span>
      </div>
    )
  }

  return (
    <div className="space-y-2">
      {Object.entries(bySource).map(([source, events]) => {
        const allChecked = isCategoryChecked(events)
        return (
          <Card key={source} className="transition-colors hover:border-border">
            <CardContent className="p-3 flex items-center gap-3">
              <Checkbox
                checked={allChecked}
                onChange={(e) => toggleCategory(events, e.target.checked)}
                className="h-4 w-4"
              />
              <div className="flex-1 min-w-0">
                <span className="text-sm font-medium text-text-primary capitalize">{source}</span>
                <span className="text-xs text-text-tertiary ml-2">
                  {events.length} event{events.length !== 1 ? 's' : ''}
                </span>
              </div>
              <span className={cn(
                'text-xs font-mono uppercase tracking-wider px-2 py-0.5 rounded-full border',
                allChecked
                  ? 'text-accent border-accent/30 bg-accent-dim'
                  : 'text-text-tertiary border-border bg-bg-hover',
              )}>
                {allChecked ? 'on' : 'off'}
              </span>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}

// ─── Main Tab ─────────────────────────────────────────────────────────────────

export default function NotificationsTab() {
  const { data: config, isLoading } = useNotificationConfig()
  const updateConfig = useUpdateNotificationConfig()

  const [localConfig, setLocalConfig] = useState<NotificationConfig>(DEFAULT_CHANNEL_CONFIG)
  const [configDirty, setConfigDirty] = useState(false)
  const [webhookErrors, setWebhookErrors] = useState({ url: '', secret: '' })

  const upsertPrefs = useUpsertNotificationPreferences()
  const [prefsDirty, setPrefsDirty] = useState(false)
  const simpleMatrixRef = useMemo(
    () => ({ current: {} as Record<string, NotificationPreference> }),
    [],
  )

  const [view, setView] = useState<'simple' | 'advanced'>('simple')

  useEffect(() => {
    if (config && !Array.isArray(config)) {
      setLocalConfig(config)
      setConfigDirty(false)
    }
  }, [config])

  const toggleChannel = (key: string) => {
    setLocalConfig((c) => ({
      ...c,
      channels: { ...c.channels, [key]: !c.channels[key as keyof typeof c.channels] },
    }))
    setConfigDirty(true)
  }

  const updateWebhookUrl = (url: string) => {
    setLocalConfig((c) => ({ ...c, webhookUrl: url }))
    setWebhookErrors((e) => ({ ...e, url: validateWebhookUrl(url) }))
    setConfigDirty(true)
  }

  const updateWebhookSecret = (secret: string) => {
    setLocalConfig((c) => ({ ...c, webhookSecret: secret }))
    setWebhookErrors((e) => ({ ...e, secret: validateWebhookSecret(secret) }))
    setConfigDirty(true)
  }

  const handleConfigSave = async () => {
    if (localConfig.channels.webhook) {
      const urlErr = validateWebhookUrl(localConfig.webhookUrl ?? '')
      const secretErr = validateWebhookSecret(localConfig.webhookSecret ?? '')
      if (urlErr || secretErr) {
        setWebhookErrors({ url: urlErr, secret: secretErr })
        return
      }
    }
    try {
      await updateConfig.mutateAsync(localConfig)
      setConfigDirty(false)
    } catch {
      // handled by api interceptor
    }
  }

  const handlePrefsSave = async () => {
    try {
      await upsertPrefs.mutateAsync(Object.values(simpleMatrixRef.current))
      setPrefsDirty(false)
    } catch {
      // handled by api interceptor
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-3">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-16 w-full" />)}
        </div>
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Section 1: Channel Config */}
      <ChannelConfigSection
        localConfig={localConfig}
        isDirty={configDirty}
        isSaving={updateConfig.isPending}
        webhookErrors={webhookErrors}
        onToggleChannel={toggleChannel}
        onWebhookUrl={updateWebhookUrl}
        onWebhookSecret={updateWebhookSecret}
        onSave={handleConfigSave}
      />

      {/* Section 2: View toggle */}
      <div className="flex items-center justify-between">
        <h2 className="text-xs font-medium uppercase tracking-wider text-text-tertiary">
          Notification Preferences
        </h2>
        <div className="flex items-center gap-1 rounded-[var(--radius-sm)] border border-border bg-bg-elevated p-0.5">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setView('simple')}
            className={cn(
              'h-7 px-3 text-xs rounded-[var(--radius-sm)] transition-colors',
              view === 'simple' ? 'bg-accent text-bg-surface font-medium' : 'text-text-secondary hover:text-text-primary',
            )}
          >
            Simple
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setView('advanced')}
            className={cn(
              'h-7 px-3 text-xs rounded-[var(--radius-sm)] transition-colors',
              view === 'advanced' ? 'bg-accent text-bg-surface font-medium' : 'text-text-secondary hover:text-text-primary',
            )}
          >
            Advanced
          </Button>
        </div>
      </div>

      {/* Section 3a: Simple */}
      {view === 'simple' && (
        <div className="space-y-3">
          <NotificationPreferencesSimple
            onDirty={() => setPrefsDirty(true)}
            matrixRef={simpleMatrixRef}
          />
          {prefsDirty && (
            <div className="flex justify-end">
              <Button size="sm" onClick={handlePrefsSave} disabled={upsertPrefs.isPending} className="gap-1.5">
                {upsertPrefs.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                Save Preferences
              </Button>
            </div>
          )}
        </div>
      )}

      {/* Section 3b: Advanced */}
      {view === 'advanced' && <NotificationPreferencesPanel />}
    </div>
  )
}
