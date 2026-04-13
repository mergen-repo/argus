import { useState } from 'react'
import { RefreshCw, AlertCircle, Plus, Trash2, Calendar } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
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
import { useMaintenanceWindows, useCreateMaintenanceWindow, useDeleteMaintenanceWindow } from '@/hooks/use-admin'
import { useAuthStore } from '@/stores/auth'
import type { CreateMaintenanceWindowRequest } from '@/types/admin'

function stateVariant(state: string): 'default' | 'success' | 'warning' | 'danger' {
  switch (state) {
    case 'active': return 'success'
    case 'scheduled': return 'warning'
    case 'completed': return 'default'
    case 'cancelled': return 'danger'
    default: return 'default'
  }
}

function formatWindow(starts: string, ends: string) {
  const s = new Date(starts)
  const e = new Date(ends)
  return `${s.toLocaleDateString()} ${s.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} — ${e.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}

const defaultForm: CreateMaintenanceWindowRequest = {
  title: '',
  description: '',
  starts_at: '',
  ends_at: '',
  affected_services: [],
}

export default function MaintenancePage() {
  const user = useAuthStore((s) => s.user)
  const isSuperAdmin = user?.role === 'super_admin'
  const [activeOnly, setActiveOnly] = useState(false)
  const [panelOpen, setPanelOpen] = useState(false)
  const [form, setForm] = useState<CreateMaintenanceWindowRequest>(defaultForm)
  const [servicesInput, setServicesInput] = useState('')
  const [formError, setFormError] = useState('')


  const { data: windows, isLoading, isError, refetch } = useMaintenanceWindows(activeOnly || undefined)
  const createMutation = useCreateMaintenanceWindow()
  const deleteMutation = useDeleteMaintenanceWindow()

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

  function handleCreate() {
    if (!form.title.trim()) { setFormError('Title is required.'); return }
    if (!form.starts_at) { setFormError('Start time is required.'); return }
    if (!form.ends_at) { setFormError('End time is required.'); return }
    if (new Date(form.ends_at) <= new Date(form.starts_at)) {
      setFormError('End time must be after start time.')
      return
    }
    const services = servicesInput.split(',').map((s) => s.trim()).filter(Boolean)
    createMutation.mutate(
      { ...form, affected_services: services },
      {
        onSuccess: () => {
          setPanelOpen(false)
          setForm(defaultForm)
          setServicesInput('')
          setFormError('')
        },
        onError: () => setFormError('Failed to create maintenance window.'),
      }
    )
  }

  return (
    <div className="space-y-6 p-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold text-text-primary">Maintenance Windows</h1>
          <p className="text-sm text-text-secondary mt-0.5">Schedule and manage planned downtime</p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setActiveOnly((v) => !v)}
          >
            {activeOnly ? 'Show all' : 'Active only'}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button size="sm" onClick={() => { setPanelOpen(true); setFormError('') }}>
            <Plus className="h-4 w-4 mr-1" />
            Schedule
          </Button>
        </div>
      </div>

      {isError && (
        <div className="flex items-center gap-2 rounded-lg border border-danger/30 bg-danger-dim p-3 text-sm text-danger">
          <AlertCircle className="h-4 w-4 shrink-0" />
          Failed to load maintenance windows.
        </div>
      )}

      <Card className="bg-bg-surface border-border">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-12 rounded-lg" />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Title</TableHead>
                <TableHead>Window</TableHead>
                <TableHead>Services</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Scope</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {(windows ?? []).length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center py-12 text-text-tertiary">
                    <Calendar className="h-8 w-8 mx-auto mb-2 opacity-30" />
                    No maintenance windows found.
                  </TableCell>
                </TableRow>
              ) : (
                (windows ?? []).map((w) => (
                  <TableRow key={w.id}>
                    <TableCell>
                      <div className="font-medium text-text-primary">{w.title}</div>
                      {w.description && (
                        <div className="text-xs text-text-tertiary truncate max-w-[200px]">{w.description}</div>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-text-secondary whitespace-nowrap">
                      {formatWindow(w.starts_at, w.ends_at)}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {(w.affected_services ?? []).slice(0, 3).map((svc) => (
                          <Badge key={svc} variant="outline" className="text-xs">{svc}</Badge>
                        ))}
                        {(w.affected_services ?? []).length > 3 && (
                          <Badge variant="outline" className="text-xs">+{w.affected_services.length - 3}</Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={stateVariant(w.state)}>{w.state}</Badge>
                    </TableCell>
                    <TableCell className="text-xs text-text-tertiary">
                      {w.tenant_id ? w.tenant_id.slice(0, 8) : 'Global'}
                    </TableCell>
                    <TableCell>
                      {w.state !== 'completed' && (
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-danger hover:text-danger/80"
                          disabled={deleteMutation.isPending}
                          onClick={() => deleteMutation.mutate(w.id)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
      </Card>

      <SlidePanel
        open={panelOpen}
        onOpenChange={setPanelOpen}
        title="Schedule Maintenance Window"
        width="sm"
      >
        <div className="space-y-4">
          <div className="space-y-1.5">
            <label htmlFor="mw-title" className="text-sm font-medium text-text-primary">
              Title <span className="text-danger">*</span>
            </label>
            <Input
              id="mw-title"
              value={form.title}
              onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
              placeholder="e.g. RADIUS maintenance"
            />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="mw-desc" className="text-sm font-medium text-text-primary">Description</label>
            <Textarea
              id="mw-desc"
              value={form.description}
              onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
              rows={2}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <label htmlFor="mw-start" className="text-sm font-medium text-text-primary">
                Starts at <span className="text-danger">*</span>
              </label>
              <Input
                id="mw-start"
                type="datetime-local"
                value={form.starts_at}
                onChange={(e) => setForm((f) => ({ ...f, starts_at: e.target.value }))}
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="mw-end" className="text-sm font-medium text-text-primary">
                Ends at <span className="text-danger">*</span>
              </label>
              <Input
                id="mw-end"
                type="datetime-local"
                value={form.ends_at}
                onChange={(e) => setForm((f) => ({ ...f, ends_at: e.target.value }))}
              />
            </div>
          </div>
          <div className="space-y-1.5">
            <label htmlFor="mw-services" className="text-sm font-medium text-text-primary">
              Affected services (comma-separated)
            </label>
            <Input
              id="mw-services"
              value={servicesInput}
              onChange={(e) => setServicesInput(e.target.value)}
              placeholder="radius, api, notifications"
            />
          </div>
          {formError && <p className="text-xs text-danger">{formError}</p>}
          <div className="flex justify-end gap-2">
            <Button variant="outline" onClick={() => setPanelOpen(false)}>Cancel</Button>
            <Button disabled={createMutation.isPending} onClick={handleCreate}>
              {createMutation.isPending ? 'Saving…' : 'Create'}
            </Button>
          </div>
        </div>
      </SlidePanel>
    </div>
  )
}
