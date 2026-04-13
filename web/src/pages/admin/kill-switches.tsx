import { useState } from 'react'
import { RefreshCw, AlertCircle, ToggleLeft, ToggleRight } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { SlidePanel } from '@/components/ui/slide-panel'
import { Textarea } from '@/components/ui/textarea'
import { useKillSwitches, useToggleKillSwitch } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import { cn } from '@/lib/utils'
import type { KillSwitch } from '@/types/admin'

function formatDate(iso: string | null) {
  if (!iso) return '—'
  return new Date(iso).toLocaleString()
}

export default function KillSwitchesPage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'

  const { data: switches, isLoading, isError, refetch } = useKillSwitches()
  const toggleMutation = useToggleKillSwitch()

  const [panelOpen, setPanelOpen] = useState(false)
  const [selected, setSelected] = useState<KillSwitch | null>(null)
  const [reason, setReason] = useState('')
  const [error, setError] = useState('')

  if (!isSuperAdmin) {
    return (
      <div className="flex flex-col items-center justify-center py-24">
        <div className="rounded-xl border border-border bg-bg-surface p-8 text-center">
          <AlertCircle className="h-10 w-10 text-text-tertiary mx-auto mb-3" />
          <p className="text-sm text-text-secondary">super_admin role required.</p>
        </div>
      </div>
    )
  }

  function openPanel(sw: KillSwitch) {
    setSelected(sw)
    setReason('')
    setError('')
    setPanelOpen(true)
  }

  function handleToggle() {
    if (!selected) return
    const enabling = !selected.enabled
    if (enabling && !reason.trim()) {
      setError('A reason is required when enabling a kill switch.')
      return
    }
    toggleMutation.mutate(
      { key: selected.key, payload: { enabled: enabling, reason: reason.trim() || undefined } },
      {
        onSuccess: () => {
          setPanelOpen(false)
          setSelected(null)
        },
        onError: () => {
          setError('Failed to toggle kill switch.')
        },
      }
    )
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Kill Switches</h1>
          <p className="text-sm text-text-secondary mt-0.5">Emergency circuit breakers — use with caution</p>
        </div>
        <Button variant="ghost" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      {(switches ?? []).some((s) => s.enabled) && (
        <div className="flex items-center gap-2 rounded-lg border border-warning/30 bg-warning-dim p-3 text-sm text-warning">
          <AlertCircle className="h-4 w-4 shrink-0" />
          One or more kill switches are active. System behavior is degraded.
        </div>
      )}

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load kill switches.
        </div>
      )}

      <Card className="bg-bg-surface border-border">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-12 rounded-lg" />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Key</TableHead>
                <TableHead>Label</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Reason</TableHead>
                <TableHead>Last Toggled</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(switches ?? []).map((sw) => (
                <TableRow key={sw.key}>
                  <TableCell className="font-mono text-xs text-text-primary">{sw.key}</TableCell>
                  <TableCell>
                    <div className="text-sm text-text-primary">{sw.label}</div>
                    <div className="text-xs text-text-tertiary">{sw.description}</div>
                  </TableCell>
                  <TableCell>
                    {sw.enabled ? (
                      <Badge variant="danger">Enabled</Badge>
                    ) : (
                      <Badge variant="success">Disabled</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-text-secondary max-w-[200px] truncate">
                    {sw.reason || '—'}
                  </TableCell>
                  <TableCell className="text-xs text-text-tertiary">
                    {formatDate(sw.toggled_at)}
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => openPanel(sw)}
                      className={cn(sw.enabled ? 'text-danger hover:text-danger/80' : 'text-text-secondary')}
                    >
                      {sw.enabled ? (
                        <ToggleRight className="h-4 w-4" />
                      ) : (
                        <ToggleLeft className="h-4 w-4" />
                      )}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <SlidePanel
        open={panelOpen}
        onOpenChange={setPanelOpen}
        title={selected ? `${selected.enabled ? 'Disable' : 'Enable'} kill switch: ${selected.key}` : ''}
        width="sm"
      >
        {selected && (
          <div className="space-y-4">
            <div className="rounded-lg bg-bg-muted p-3 text-sm text-text-secondary">
              {selected.description}
            </div>

            {!selected.enabled && (
              <div className="space-y-1.5">
                <label htmlFor="ks-reason" className="text-sm font-medium text-text-primary">
                  Reason <span className="text-danger">*</span>
                </label>
                <Textarea
                  id="ks-reason"
                  placeholder="Explain why you are enabling this kill switch…"
                  value={reason}
                  onChange={(e) => {
                    setReason(e.target.value)
                    setError('')
                  }}
                  rows={3}
                />
                {error && <p className="text-xs text-danger">{error}</p>}
              </div>
            )}

            {selected.enabled && (
              <div className="space-y-1.5">
                <label htmlFor="ks-reason-disable" className="text-sm font-medium text-text-primary">
                  Reason (optional)
                </label>
                <Textarea
                  id="ks-reason-disable"
                  placeholder="Explain why you are disabling this kill switch…"
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  rows={2}
                />
              </div>
            )}

            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setPanelOpen(false)}>
                Cancel
              </Button>
              <Button
                variant={selected.enabled ? 'default' : 'destructive'}
                disabled={toggleMutation.isPending}
                onClick={handleToggle}
              >
                {toggleMutation.isPending
                  ? 'Saving…'
                  : selected.enabled
                  ? 'Disable'
                  : 'Enable'}
              </Button>
            </div>
          </div>
        )}
      </SlidePanel>
    </div>
  )
}
