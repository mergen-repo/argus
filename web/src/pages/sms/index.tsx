import { useState } from 'react'
import { toast } from 'sonner'
import { Send, MessageSquare, Search, Loader2 } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Select } from '@/components/ui/select'
import { Breadcrumb } from '@/components/ui/breadcrumb'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { useSendSMS, useSMSHistory } from '@/hooks/use-sms'

const STATUS_BADGE: Record<string, 'success' | 'warning' | 'danger' | 'outline'> = {
  sent: 'success',
  delivered: 'success',
  queued: 'outline',
  failed: 'danger',
  rate_limited: 'warning',
}

const PRIORITY_OPTIONS = [
  { value: 'low', label: 'Low' },
  { value: 'normal', label: 'Normal' },
  { value: 'high', label: 'High' },
]

const STATUS_FILTER_OPTIONS = [
  { value: '', label: 'All statuses' },
  { value: 'queued', label: 'Queued' },
  { value: 'sent', label: 'Sent' },
  { value: 'delivered', label: 'Delivered' },
  { value: 'failed', label: 'Failed' },
]

export default function SMSPage() {
  const [simID, setSimID] = useState('')
  const [text, setText] = useState('')
  const [priority, setPriority] = useState('normal')
  const [statusFilter, setStatusFilter] = useState('')
  const [simSearch, setSimSearch] = useState('')

  const send = useSendSMS()
  const historyQuery = useSMSHistory({
    sim_id: simSearch || undefined,
    status: statusFilter || undefined,
  })
  const history = historyQuery.data?.data ?? []

  const handleSend = async () => {
    if (!simID || !text) {
      toast.error('SIM ID and message text are required')
      return
    }
    if (text.length > 480) {
      toast.error('Message exceeds 480 characters')
      return
    }
    try {
      await send.mutateAsync({ sim_id: simID, text, priority })
      toast.success('SMS queued for delivery')
      setText('')
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to queue SMS'
      toast.error(msg)
    }
  }

  return (
    <div className="space-y-6">
      <Breadcrumb items={[{ label: 'Dashboard', href: '/' }, { label: 'SMS Gateway' }]} />
      <div>
        <h1 className="text-[16px] font-semibold text-text-primary">SMS Gateway</h1>
        <p className="text-xs text-text-tertiary mt-0.5">Send SMS to SIMs and review delivery history</p>
      </div>

      <Card className="p-5">
        <div className="flex items-center gap-2 mb-4">
          <Send className="h-4 w-4 text-accent" />
          <h2 className="text-sm font-semibold text-text-primary">Send SMS</h2>
        </div>
        <div className="grid gap-4">
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">Target SIM ID *</label>
            <Input
              type="text"
              placeholder="00000000-0000-0000-0000-000000000000"
              value={simID}
              onChange={(e) => setSimID(e.target.value)}
              className="font-mono text-sm"
            />
          </div>
          <div>
            <label className="text-xs font-medium text-text-secondary mb-1.5 block">
              Message ({text.length}/480)
            </label>
            <Textarea
              placeholder="Enter your message"
              value={text}
              onChange={(e) => setText(e.target.value)}
              maxLength={480}
              rows={4}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs font-medium text-text-secondary mb-1.5 block">Priority</label>
              <Select
                value={priority}
                onChange={(e) => setPriority(e.target.value)}
                options={PRIORITY_OPTIONS}
              />
            </div>
          </div>
          <Button onClick={handleSend} disabled={send.isPending} className="gap-1.5 self-end">
            {send.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
            {send.isPending ? 'Sending...' : 'Send SMS'}
          </Button>
        </div>
      </Card>

      <Card>
        <div className="px-5 py-4 border-b border-border flex items-center justify-between gap-4">
          <div className="flex items-center gap-2">
            <MessageSquare className="h-4 w-4 text-text-tertiary" />
            <h2 className="text-sm font-semibold text-text-primary">SMS History</h2>
            <Badge variant="outline" className="text-[10px]">{history.length}</Badge>
          </div>
          <div className="flex items-center gap-2">
            <div className="relative">
              <Search className="h-3 w-3 absolute left-2 top-1/2 -translate-y-1/2 text-text-tertiary" />
              <Input
                placeholder="Filter by SIM ID"
                value={simSearch}
                onChange={(e) => setSimSearch(e.target.value)}
                className="pl-7 h-8 text-xs w-[260px] font-mono"
              />
            </div>
            <Select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value)}
              options={STATUS_FILTER_OPTIONS}
              className="h-8 text-xs w-[140px]"
            />
          </div>
        </div>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Queued</TableHead>
              <TableHead>SIM</TableHead>
              <TableHead>MSISDN</TableHead>
              <TableHead>Preview</TableHead>
              <TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {history.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-text-tertiary py-8">
                  No SMS messages yet.
                </TableCell>
              </TableRow>
            )}
            {history.map((m) => (
              <TableRow key={m.id}>
                <TableCell className="text-xs font-mono text-text-secondary">{new Date(m.queued_at).toLocaleString()}</TableCell>
                <TableCell><code className="text-xs font-mono">{m.sim_id.slice(0, 8)}…</code></TableCell>
                <TableCell><code className="text-xs font-mono">{m.msisdn}</code></TableCell>
                <TableCell><span className="text-xs text-text-secondary">{m.text_preview}</span></TableCell>
                <TableCell>
                  <Badge variant={STATUS_BADGE[m.status] ?? 'outline'} className="text-[10px]">
                    {m.status}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  )
}
