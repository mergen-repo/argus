import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Save, Loader2 } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Select } from '@/components/ui/select'
import {
  useNotificationTemplates,
  useUpsertNotificationTemplate,
  type NotificationTemplate,
} from '@/hooks/use-notification-preferences'

const EVENT_TYPES = [
  'sim.activated',
  'sim.suspended',
  'session.ended',
  'anomaly.detected',
  'policy.violation',
  'webhook.dead_letter',
  'report_ready',
]

const LOCALES = [
  { value: 'en', label: 'English (en)' },
  { value: 'tr', label: 'Türkçe (tr)' },
]

function blankTemplate(eventType: string, locale: string): NotificationTemplate {
  return { event_type: eventType, locale, subject: '', body_text: '', body_html: '' }
}

export function NotificationTemplatesPanel() {
  const [eventType, setEventType] = useState(EVENT_TYPES[0])
  const [locale, setLocale] = useState('en')
  const query = useNotificationTemplates(eventType, locale)
  const upsert = useUpsertNotificationTemplate()
  const [draft, setDraft] = useState<NotificationTemplate>(blankTemplate(eventType, locale))

  useEffect(() => {
    if (query.data && query.data.length > 0) {
      setDraft(query.data[0])
    } else {
      setDraft(blankTemplate(eventType, locale))
    }
  }, [query.data, eventType, locale])

  const handleSave = async () => {
    try {
      await upsert.mutateAsync({ ...draft, event_type: eventType, locale })
      toast.success('Template saved')
    } catch {
      toast.error('Failed to save template')
    }
  }

  return (
    <Card className="mt-3 p-4">
      <div className="flex items-end gap-3 mb-4">
        <div className="flex-1 max-w-[260px]">
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Event Type</label>
          <Select
            value={eventType}
            onChange={(e) => setEventType(e.target.value)}
            options={EVENT_TYPES.map((e) => ({ value: e, label: e }))}
            className="h-8 text-xs"
          />
        </div>
        <div className="w-[200px]">
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Locale</label>
          <Select
            value={locale}
            onChange={(e) => setLocale(e.target.value)}
            options={LOCALES}
            className="h-8 text-xs"
          />
        </div>
        <Button onClick={handleSave} disabled={upsert.isPending} className="gap-1.5">
          {upsert.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
          Save
        </Button>
      </div>

      <div className="space-y-3">
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Subject</label>
          <Input
            value={draft.subject}
            onChange={(e) => setDraft({ ...draft, subject: e.target.value })}
            placeholder="{{ .EntityID }} suspended"
          />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Body (Plain Text)</label>
          <Textarea
            value={draft.body_text}
            onChange={(e) => setDraft({ ...draft, body_text: e.target.value })}
            rows={6}
            placeholder="Hello {{ .UserName }}, your SIM was suspended..."
          />
        </div>
        <div>
          <label className="text-xs font-medium text-text-secondary mb-1.5 block">Body (HTML, optional)</label>
          <Textarea
            value={draft.body_html}
            onChange={(e) => setDraft({ ...draft, body_html: e.target.value })}
            rows={6}
            placeholder="<p>Hello {{ .UserName }}...</p>"
            className="font-mono text-xs"
          />
        </div>
      </div>

      <p className="text-[11px] text-text-tertiary mt-3">
        Templates use Go's text/template syntax. Available fields: <code>.TenantName</code>, <code>.UserName</code>, <code>.EventTime</code>, <code>.EntityID</code>, <code>.ExtraFields.<i>key</i></code>.
      </p>
    </Card>
  )
}
