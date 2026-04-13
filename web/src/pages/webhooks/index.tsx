import { useState } from 'react'
import { toast } from 'sonner'
import { Plus, Trash2, RefreshCw, Webhook as WebhookIcon, CheckCircle2, AlertCircle, Copy } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import {
  useWebhookConfigs,
  useCreateWebhook,
  useDeleteWebhook,
  useWebhookDeliveries,
  useRetryWebhookDelivery,
  type WebhookConfig,
} from '@/hooks/use-webhooks'

const EVENT_TYPE_OPTIONS = [
  'sim.activated',
  'sim.suspended',
  'sim.terminated',
  'session.started',
  'session.ended',
  'anomaly.detected',
  'policy.violation',
  'operator.health_changed',
]

function CreateWebhookDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onCreated: (secret: string) => void
}) {
  const create = useCreateWebhook()
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [eventTypes, setEventTypes] = useState<string[]>([])

  const reset = () => {
    setUrl('')
    setSecret('')
    setEventTypes([])
  }

  const handleSubmit = async () => {
    if (!url.startsWith('https://')) {
      toast.error('Webhook URL must use https://')
      return
    }
    if (!secret) {
      toast.error('Secret required for HMAC signing')
      return
    }
    try {
      const cfg = await create.mutateAsync({ url, secret, event_types: eventTypes, enabled: true })
      onCreated(cfg.secret ?? secret)
      onOpenChange(false)
      reset()
    } catch {
      toast.error('Failed to create webhook')
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) reset(); onOpenChange(v) }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New Webhook</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">Endpoint URL *</label>
            <Input
              type="url"
              placeholder="https://example.com/webhook"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
            <p className="text-[11px] text-text-tertiary mt-1">https only — for HMAC integrity</p>
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">Shared Secret *</label>
            <Input
              type="text"
              placeholder="signing secret (shown only once)"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
            />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary mb-2 block">Event Types</label>
            <div className="grid grid-cols-2 gap-2">
              {EVENT_TYPE_OPTIONS.map((evt) => (
                <label key={evt} className="flex items-center gap-2 text-xs text-text-secondary cursor-pointer">
                  <Checkbox
                    checked={eventTypes.includes(evt)}
                    onChange={(e) => {
                      setEventTypes((prev) => e.target.checked ? [...prev, evt] : prev.filter((p) => p !== evt))
                    }}
                  />
                  <span className="font-mono">{evt}</span>
                </label>
              ))}
            </div>
            <p className="text-[11px] text-text-tertiary mt-2">Empty = subscribe to all events</p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button size="sm" onClick={handleSubmit} disabled={create.isPending}>
            {create.isPending ? 'Creating...' : 'Create'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function DeliveryLogPanel({ config, onClose }: { config: WebhookConfig | null; onClose: () => void }) {
  const open = !!config
  const deliveriesQuery = useWebhookDeliveries(config?.id ?? null)
  const retry = useRetryWebhookDelivery()
  const deliveries = deliveriesQuery.data?.data ?? []

  const handleRetry = async (deliveryID: string) => {
    if (!config) return
    try {
      await retry.mutateAsync({ configID: config.id, deliveryID })
      toast.success('Retry queued')
    } catch {
      toast.error('Failed to retry delivery')
    }
  }

  return (
    <SlidePanel
      open={open}
      onOpenChange={(v) => { if (!v) onClose() }}
      title="Delivery Log"
      description={config?.url}
      width="lg"
    >
      <div className="space-y-2">
        {deliveriesQuery.isLoading && <p className="text-sm text-text-tertiary">Loading deliveries...</p>}
        {!deliveriesQuery.isLoading && deliveries.length === 0 && (
          <p className="text-sm text-text-tertiary">No deliveries yet.</p>
        )}
        {deliveries.map((d) => (
          <div key={d.id} className="border border-border rounded-md p-3 flex items-start justify-between gap-3 hover:bg-bg-hover/50">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 mb-1">
                <Badge variant={d.final_state === 'succeeded' ? 'success' : d.final_state === 'dead_letter' ? 'danger' : 'warning'} className="text-[10px]">
                  {d.final_state}
                </Badge>
                <span className="text-xs font-mono text-text-secondary truncate">{d.event_type}</span>
              </div>
              <div className="text-[11px] text-text-tertiary font-mono">
                attempt {d.attempt_count} · status {d.response_status ?? '—'} · {new Date(d.created_at).toLocaleString()}
              </div>
            </div>
            <Button size="sm" variant="ghost" onClick={() => handleRetry(d.id)} disabled={retry.isPending} className="h-7 gap-1">
              <RefreshCw className="h-3 w-3" />
              Retry
            </Button>
          </div>
        ))}
      </div>
      <SlidePanelFooter className="mt-4">
        <Button variant="outline" size="sm" onClick={onClose}>Close</Button>
      </SlidePanelFooter>
    </SlidePanel>
  )
}

export default function WebhooksPage() {
  const configsQuery = useWebhookConfigs()
  const remove = useDeleteWebhook()
  const [createOpen, setCreateOpen] = useState(false)
  const [secretJustCreated, setSecretJustCreated] = useState<string | null>(null)
  const [deliveryFor, setDeliveryFor] = useState<WebhookConfig | null>(null)

  const configs = configsQuery.data?.data ?? []

  const handleDelete = async (id: string) => {
    if (!window.confirm('Delete this webhook?')) return
    try {
      await remove.mutateAsync(id)
      toast.success('Webhook deleted')
    } catch {
      toast.error('Failed to delete webhook')
    }
  }

  return (
    <div className="space-y-6">
      <Breadcrumb items={[{ label: 'Dashboard', href: '/' }, { label: 'Webhooks' }]} />
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-[16px] font-semibold text-text-primary">Webhooks</h1>
          <p className="text-xs text-text-tertiary mt-0.5">HMAC-signed event delivery to external endpoints</p>
        </div>
        <Button size="sm" className="gap-1.5" onClick={() => setCreateOpen(true)}>
          <Plus className="h-3.5 w-3.5" />
          New Webhook
        </Button>
      </div>

      {secretJustCreated && (
        <Card className="border-warning bg-warning-dim p-4">
          <div className="flex items-start gap-3">
            <AlertCircle className="h-5 w-5 text-warning flex-shrink-0 mt-0.5" />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium text-text-primary">Save this secret — it will not be shown again.</p>
              <div className="mt-2 flex items-center gap-2">
                <code className="text-xs font-mono bg-bg-elevated px-2 py-1 rounded select-all flex-1 truncate">
                  {secretJustCreated}
                </code>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => { navigator.clipboard.writeText(secretJustCreated); toast.success('Copied') }}
                  className="gap-1.5"
                >
                  <Copy className="h-3 w-3" />
                  Copy
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setSecretJustCreated(null)}>Dismiss</Button>
              </div>
            </div>
          </div>
        </Card>
      )}

      <Card>
        {configs.length === 0 && !configsQuery.isLoading ? (
          <div className="p-8 text-center">
            <WebhookIcon className="h-10 w-10 text-text-tertiary mx-auto mb-2" />
            <p className="text-sm text-text-secondary">No webhooks configured yet.</p>
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>URL</TableHead>
                <TableHead>Events</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last Success</TableHead>
                <TableHead>Failures</TableHead>
                <TableHead className="w-[180px]">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {configs.map((c) => (
                <TableRow key={c.id}>
                  <TableCell>
                    <code className="text-xs font-mono text-text-primary truncate max-w-[280px] block">{c.url}</code>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {c.event_types.length === 0 ? (
                        <Badge variant="outline" className="text-[10px]">all</Badge>
                      ) : (
                        c.event_types.slice(0, 3).map((e) => (
                          <Badge key={e} variant="outline" className="text-[10px]">{e}</Badge>
                        ))
                      )}
                      {c.event_types.length > 3 && (
                        <Badge variant="outline" className="text-[10px]">+{c.event_types.length - 3}</Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    {c.enabled ? (
                      <Badge variant="success" className="text-[10px] gap-1"><CheckCircle2 className="h-2.5 w-2.5" />Enabled</Badge>
                    ) : (
                      <Badge variant="warning" className="text-[10px]">Disabled</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    <span className="text-xs font-mono text-text-secondary">
                      {c.last_success_at ? new Date(c.last_success_at).toLocaleString() : '—'}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className={`text-xs font-mono ${c.failure_count > 0 ? 'text-error' : 'text-text-tertiary'}`}>
                      {c.failure_count}
                    </span>
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1">
                      <Button size="sm" variant="ghost" onClick={() => setDeliveryFor(c)} className="h-7 text-xs">
                        Deliveries
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => handleDelete(c.id)} className="h-7 w-7 p-0 text-error hover:text-error">
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <CreateWebhookDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(secret) => setSecretJustCreated(secret)}
      />
      <DeliveryLogPanel config={deliveryFor} onClose={() => setDeliveryFor(null)} />
    </div>
  )
}
