// Channel config card for NotificationsTab. Lifted from pages/settings/notifications.tsx (FIX-240).
import { Save, Loader2, Mail, MessageSquare, Webhook, Smartphone } from 'lucide-react'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import type { NotificationConfig } from '@/types/settings'

export const CHANNEL_META: Record<string, { icon: React.ElementType; label: string; description: string }> = {
  email: { icon: Mail, label: 'Email', description: 'Receive notifications via email' },
  telegram: { icon: MessageSquare, label: 'Telegram', description: 'Receive alerts in Telegram' },
  webhook: { icon: Webhook, label: 'Webhook', description: 'POST events to a webhook URL' },
  sms: { icon: Smartphone, label: 'SMS', description: 'Get SMS alerts for critical events' },
}

export const DEFAULT_CHANNEL_CONFIG: NotificationConfig = {
  channels: { email: true, telegram: false, webhook: false, sms: false },
  webhookUrl: '',
  webhookSecret: '',
  subscriptions: [],
  thresholds: [],
}

export function validateWebhookUrl(url: string) {
  if (!url) return 'Webhook URL is required'
  if (!url.startsWith('https://')) return 'Webhook URL must start with https://'
  return ''
}

export function validateWebhookSecret(secret: string) {
  if (!secret) return 'Webhook secret is required'
  return ''
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: () => void }) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      onClick={onChange}
      className={cn(
        'relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent p-0',
        checked ? 'bg-accent hover:bg-accent' : 'bg-bg-hover hover:bg-bg-hover',
      )}
    >
      <span
        className={cn(
          'inline-block h-3.5 w-3.5 rounded-full bg-text-primary transition-transform shadow-sm',
          checked ? 'translate-x-5' : 'translate-x-0.5',
        )}
      />
    </Button>
  )
}

interface Props {
  localConfig: NotificationConfig
  isDirty: boolean
  isSaving: boolean
  webhookErrors: { url: string; secret: string }
  onToggleChannel: (key: string) => void
  onWebhookUrl: (v: string) => void
  onWebhookSecret: (v: string) => void
  onSave: () => void
}

export function ChannelConfigSection({
  localConfig, isDirty, isSaving, webhookErrors,
  onToggleChannel, onWebhookUrl, onWebhookSecret, onSave,
}: Props) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-xs font-medium uppercase tracking-wider text-text-tertiary">
          Delivery Channels
        </h2>
        {isDirty && (
          <Button size="sm" onClick={onSave} disabled={isSaving} className="gap-1.5">
            {isSaving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
            Save
          </Button>
        )}
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        {Object.entries(CHANNEL_META).map(([key, meta]) => {
          const Icon = meta.icon
          const enabled = !!localConfig.channels[key as keyof typeof localConfig.channels]
          return (
            <Card
              key={key}
              className={cn('cursor-pointer transition-colors', enabled && 'border-accent/30')}
              onClick={() => onToggleChannel(key)}
            >
              <CardContent className="p-4 flex items-center gap-4">
                <div className={cn(
                  'h-9 w-9 rounded-[var(--radius-sm)] flex items-center justify-center shrink-0',
                  enabled ? 'bg-accent-dim text-accent' : 'bg-bg-hover text-text-tertiary',
                )}>
                  <Icon className="h-4 w-4" />
                </div>
                <div className="flex-1 min-w-0">
                  <span className="text-sm font-medium text-text-primary block">{meta.label}</span>
                  <span className="text-xs text-text-secondary">{meta.description}</span>
                </div>
                <Toggle checked={enabled} onChange={() => onToggleChannel(key)} />
              </CardContent>
            </Card>
          )
        })}
      </div>
      {localConfig.channels.webhook && (
        <Card>
          <CardContent className="p-4 space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-text-secondary">Webhook URL</label>
              <Input
                type="url"
                placeholder="https://your-server.com/webhook"
                value={localConfig.webhookUrl ?? ''}
                onChange={(e) => onWebhookUrl(e.target.value)}
                className={cn(webhookErrors.url && 'border-danger')}
              />
              {webhookErrors.url && <p className="text-xs text-danger">{webhookErrors.url}</p>}
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-text-secondary">Webhook Secret</label>
              <Input
                type="password"
                placeholder="Signing secret for HMAC verification"
                value={localConfig.webhookSecret ?? ''}
                onChange={(e) => onWebhookSecret(e.target.value)}
                className={cn(webhookErrors.secret && 'border-danger')}
              />
              {webhookErrors.secret && <p className="text-xs text-danger">{webhookErrors.secret}</p>}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
