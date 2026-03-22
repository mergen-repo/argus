import { useState, useEffect } from 'react'
import {
  AlertCircle,
  RefreshCw,
  Loader2,
  Save,
  Mail,
  MessageSquare,
  Webhook,
  Smartphone,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { useNotificationConfig, useUpdateNotificationConfig } from '@/hooks/use-settings'
import type { NotificationConfig } from '@/types/settings'
import { cn } from '@/lib/utils'

function Skeleton({ className }: { className?: string }) {
  return <div className={`animate-pulse rounded-[var(--radius-sm)] bg-bg-hover ${className ?? ''}`} />
}

const CHANNEL_META: Record<string, { icon: React.ElementType; label: string; description: string }> = {
  email: { icon: Mail, label: 'Email', description: 'Receive notifications via email' },
  telegram: { icon: MessageSquare, label: 'Telegram', description: 'Receive alerts in Telegram' },
  webhook: { icon: Webhook, label: 'Webhook', description: 'POST events to a webhook URL' },
  sms: { icon: Smartphone, label: 'SMS', description: 'Get SMS alerts for critical events' },
}

const DEFAULT_CONFIG: NotificationConfig = {
  channels: { email: true, telegram: false, webhook: false, sms: false },
  subscriptions: [
    {
      category: 'SIM Events',
      events: [
        { event: 'sim.activated', label: 'SIM Activated', enabled: true },
        { event: 'sim.suspended', label: 'SIM Suspended', enabled: true },
        { event: 'sim.terminated', label: 'SIM Terminated', enabled: true },
        { event: 'sim.stolen_lost', label: 'SIM Stolen/Lost', enabled: true },
      ],
    },
    {
      category: 'Session Events',
      events: [
        { event: 'session.started', label: 'Session Started', enabled: false },
        { event: 'session.ended', label: 'Session Ended', enabled: false },
        { event: 'session.auth_failed', label: 'Auth Failed', enabled: true },
      ],
    },
    {
      category: 'System Events',
      events: [
        { event: 'system.operator_down', label: 'Operator Down', enabled: true },
        { event: 'system.anomaly_detected', label: 'Anomaly Detected', enabled: true },
        { event: 'system.job_failed', label: 'Job Failed', enabled: true },
      ],
    },
    {
      category: 'Policy Events',
      events: [
        { event: 'policy.activated', label: 'Policy Activated', enabled: false },
        { event: 'policy.rollout_complete', label: 'Rollout Complete', enabled: true },
      ],
    },
  ],
  thresholds: [
    { key: 'quota_usage', label: 'Alert at quota usage', value: 80, min: 50, max: 100, unit: '%' },
    { key: 'error_rate', label: 'Alert at error rate', value: 5, min: 1, max: 50, unit: '%' },
    { key: 'session_count', label: 'Alert at session count', value: 10000, min: 100, max: 1000000, unit: '' },
  ],
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      onClick={() => onChange(!checked)}
      className={cn(
        'relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        checked ? 'bg-accent' : 'bg-bg-hover',
      )}
    >
      <span
        className={cn(
          'inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform shadow-sm',
          checked ? 'translate-x-[18px]' : 'translate-x-[3px]',
        )}
      />
    </button>
  )
}

export default function NotificationConfigPage() {
  const { data: config, isLoading, isError, refetch } = useNotificationConfig()
  const updateMutation = useUpdateNotificationConfig()

  const [localConfig, setLocalConfig] = useState<NotificationConfig>(DEFAULT_CONFIG)
  const [isDirty, setIsDirty] = useState(false)

  useEffect(() => {
    if (config) {
      setLocalConfig(config)
      setIsDirty(false)
    }
  }, [config])

  const toggleChannel = (channel: string) => {
    setLocalConfig((c) => ({
      ...c,
      channels: { ...c.channels, [channel]: !c.channels[channel as keyof typeof c.channels] },
    }))
    setIsDirty(true)
  }

  const toggleEvent = (categoryIdx: number, eventIdx: number) => {
    setLocalConfig((c) => {
      const newSubs = [...c.subscriptions]
      const cat = { ...newSubs[categoryIdx] }
      const events = [...cat.events]
      events[eventIdx] = { ...events[eventIdx], enabled: !events[eventIdx].enabled }
      cat.events = events
      newSubs[categoryIdx] = cat
      return { ...c, subscriptions: newSubs }
    })
    setIsDirty(true)
  }

  const updateThreshold = (idx: number, value: number) => {
    setLocalConfig((c) => {
      const newThresholds = [...c.thresholds]
      newThresholds[idx] = { ...newThresholds[idx], value }
      return { ...c, thresholds: newThresholds }
    })
    setIsDirty(true)
  }

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync(localConfig)
      setIsDirty(false)
    } catch {
      // handled by api interceptor
    }
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-4">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-8 text-center">
          <AlertCircle className="h-10 w-10 text-danger mx-auto mb-3" />
          <h2 className="text-lg font-semibold text-text-primary mb-2">Failed to load config</h2>
          <p className="text-sm text-text-secondary mb-4">Unable to fetch notification preferences.</p>
          <Button onClick={() => refetch()} variant="outline" className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="p-6 space-y-4">
        <Skeleton className="h-6 w-48" />
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Card key={i}>
              <CardHeader className="pb-2"><Skeleton className="h-4 w-24" /></CardHeader>
              <CardContent className="pt-0"><Skeleton className="h-8 w-full" /></CardContent>
            </Card>
          ))}
        </div>
        <Card>
          <CardHeader><Skeleton className="h-4 w-36" /></CardHeader>
          <CardContent><Skeleton className="h-32 w-full" /></CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Notification Config</h1>
        {isDirty && (
          <Button
            size="sm"
            className="gap-2"
            onClick={handleSave}
            disabled={updateMutation.isPending}
          >
            {updateMutation.isPending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Save className="h-3.5 w-3.5" />
            )}
            Save Changes
          </Button>
        )}
      </div>

      {/* Delivery Channels */}
      <div>
        <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">
          Delivery Channels
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {Object.entries(CHANNEL_META).map(([key, meta]) => {
            const Icon = meta.icon
            const enabled = localConfig.channels[key as keyof typeof localConfig.channels]
            return (
              <Card
                key={key}
                className={cn(
                  'cursor-pointer transition-colors',
                  enabled && 'border-accent/30',
                )}
                onClick={() => toggleChannel(key)}
              >
                <CardContent className="p-4 flex items-center gap-4">
                  <div className={cn(
                    'h-9 w-9 rounded-[var(--radius-sm)] flex items-center justify-center flex-shrink-0',
                    enabled ? 'bg-accent-dim text-accent' : 'bg-bg-hover text-text-tertiary',
                  )}>
                    <Icon className="h-4 w-4" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <span className="text-sm font-medium text-text-primary block">{meta.label}</span>
                    <span className="text-xs text-text-secondary">{meta.description}</span>
                  </div>
                  <Toggle checked={enabled} onChange={() => toggleChannel(key)} />
                </CardContent>
              </Card>
            )
          })}
        </div>
      </div>

      {/* Event Subscriptions */}
      <div>
        <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">
          Event Subscriptions
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {localConfig.subscriptions.map((category, catIdx) => (
            <Card key={category.category}>
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">{category.category}</CardTitle>
              </CardHeader>
              <CardContent className="pt-0 space-y-2">
                {category.events.map((evt, evtIdx) => (
                  <div
                    key={evt.event}
                    className="flex items-center justify-between py-1.5"
                  >
                    <span className="text-xs text-text-secondary">{evt.label}</span>
                    <Toggle
                      checked={evt.enabled}
                      onChange={() => toggleEvent(catIdx, evtIdx)}
                    />
                  </div>
                ))}
              </CardContent>
            </Card>
          ))}
        </div>
      </div>

      {/* Threshold Sliders */}
      <div>
        <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">
          Alert Thresholds
        </h2>
        <Card>
          <CardContent className="p-4 space-y-5">
            {localConfig.thresholds.map((threshold, idx) => (
              <div key={threshold.key}>
                <div className="flex items-center justify-between mb-2">
                  <span className="text-xs text-text-secondary">{threshold.label}</span>
                  <span className="font-mono text-sm text-text-primary">
                    {threshold.value.toLocaleString()}{threshold.unit}
                  </span>
                </div>
                <input
                  type="range"
                  min={threshold.min}
                  max={threshold.max}
                  value={threshold.value}
                  onChange={(e) => updateThreshold(idx, parseInt(e.target.value))}
                  className="w-full h-1.5 bg-bg-hover rounded-full appearance-none cursor-pointer accent-accent"
                />
                <div className="flex justify-between mt-1">
                  <span className="text-[10px] text-text-tertiary">{threshold.min}{threshold.unit}</span>
                  <span className="text-[10px] text-text-tertiary">{threshold.max.toLocaleString()}{threshold.unit}</span>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
