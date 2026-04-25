import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { BellOff, Trash2, Plus, AlertCircle, RefreshCw } from 'lucide-react'
import axios from 'axios'
import { toast } from 'sonner'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableHeader,
  TableBody,
  TableHead,
  TableRow,
  TableCell,
} from '@/components/ui/table'
import { Skeleton } from '@/components/ui/skeleton'
import { UnmuteDialog } from '@/pages/alerts/_partials/unmute-dialog'
import { MutePanel } from '@/pages/alerts/_partials/mute-panel'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import { Link } from 'react-router-dom'

// ─── Types ──────────────────────────────────────────────────────────────────

interface SuppressionRule {
  id: string
  tenant_id: string
  scope_type: string
  scope_value: string
  expires_at: string
  reason?: string | null
  rule_name?: string | null
  created_by?: string | null
  created_at: string
}

interface TenantSettings {
  alert_retention_days?: number
  [key: string]: unknown
}

interface TenantResponse {
  id: string
  name: string
  settings?: TenantSettings | null
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function relativeTime(ts?: string): string {
  if (!ts) return '–'
  const diff = new Date(ts).getTime() - Date.now()
  const absDiff = Math.abs(diff)
  const mins = Math.floor(absDiff / 60000)
  const hours = Math.floor(mins / 60)
  const days = Math.floor(hours / 24)
  if (diff < 0) return 'Expired'
  if (days > 0) return `${days}d left`
  if (hours > 0) return `${hours}h left`
  if (mins > 0) return `${mins}m left`
  return 'Expiring soon'
}

function formatScope(rule: SuppressionRule): string {
  switch (rule.scope_type) {
    case 'this':
      return `this=${rule.scope_value.slice(0, 8)}…`
    case 'type':
      return `type=${rule.scope_value}`
    case 'operator':
      return `operator=${rule.scope_value.slice(0, 8)}…`
    case 'dedup_key':
      return `dedup=${rule.scope_value.length > 20 ? rule.scope_value.slice(0, 20) + '…' : rule.scope_value}`
    default:
      return rule.scope_value
  }
}

const SUPPRESSIONS_KEY = ['alert-suppressions'] as const

// ─── Active Rules Section ────────────────────────────────────────────────────

function ActiveRulesSection() {
  const qc = useQueryClient()
  const [deleteTarget, setDeleteTarget] = useState<SuppressionRule | null>(null)
  const [createOpen, setCreateOpen] = useState(false)

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: [...SUPPRESSIONS_KEY, 'active'],
    queryFn: async () => {
      const res = await api.get<{ status: string; data: SuppressionRule[] }>(
        '/alerts/suppressions?active_only=true',
      )
      return res.data.data ?? []
    },
    staleTime: 30_000,
  })

  const handleDeleteSuccess = () => {
    qc.invalidateQueries({ queryKey: SUPPRESSIONS_KEY })
  }

  const handleCreateSuccess = () => {
    qc.invalidateQueries({ queryKey: SUPPRESSIONS_KEY })
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center">
        <div className="rounded-xl border border-danger/30 bg-danger-dim p-6">
          <AlertCircle className="h-8 w-8 text-danger mx-auto mb-3" />
          <h3 className="text-sm font-semibold text-text-primary mb-1">Failed to load suppression rules</h3>
          <Button onClick={() => refetch()} variant="outline" size="sm" className="gap-2 mt-2">
            <RefreshCw className="h-3.5 w-3.5" />
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <>
      <Card className="overflow-hidden density-compact">
        <CardHeader className="pb-0 px-4 pt-4">
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm font-medium text-text-primary flex items-center gap-2">
              <BellOff className="h-4 w-4 text-text-tertiary" />
              Active Suppression Rules
            </CardTitle>
            <Button
              variant="outline"
              size="sm"
              className="gap-1.5 h-7 text-xs"
              onClick={() => setCreateOpen(true)}
            >
              <Plus className="h-3.5 w-3.5" />
              Create rule
            </Button>
          </div>
        </CardHeader>

        <div className="overflow-x-auto mt-3">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Scope</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead>Created by</TableHead>
                <TableHead className="w-[60px]"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading &&
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 5 }).map((_, j) => (
                      <TableCell key={j}><Skeleton className="h-4 w-20" /></TableCell>
                    ))}
                  </TableRow>
                ))}

              {!isLoading && data && data.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5}>
                    <div className="flex flex-col items-center justify-center py-12 text-center">
                      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
                        <BellOff className="h-8 w-8 text-text-tertiary mx-auto mb-3" />
                        <h3 className="text-sm font-semibold text-text-primary mb-1">No active suppression rules</h3>
                        <p className="text-xs text-text-secondary mb-3 max-w-xs">
                          Mute an alert to create one.
                        </p>
                        <Link
                          to="/alerts"
                          className="text-xs text-accent hover:underline transition-colors"
                        >
                          Go to Alerts →
                        </Link>
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}

              {(data ?? []).map((rule) => (
                <TableRow key={rule.id}>
                  <TableCell>
                    {rule.rule_name ? (
                      <span className="text-xs font-medium text-text-primary">{rule.rule_name}</span>
                    ) : (
                      <span className="text-xs italic text-text-tertiary">Ad-hoc</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant="secondary"
                      className="font-mono text-[10px] max-w-[200px] truncate"
                      title={formatScope(rule)}
                    >
                      {formatScope(rule)}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs text-text-secondary">{relativeTime(rule.expires_at)}</span>
                  </TableCell>
                  <TableCell>
                    {rule.created_by ? (
                      <span className="font-mono text-[11px] text-text-tertiary">
                        {rule.created_by.slice(0, 8)}…
                      </span>
                    ) : (
                      <span className="text-xs text-text-tertiary">–</span>
                    )}
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 text-text-tertiary hover:text-danger transition-colors"
                      title="Remove suppression rule"
                      onClick={() => setDeleteTarget(rule)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>

        {!isLoading && data && data.length > 0 && (
          <div className="px-4 py-3 border-t border-border-subtle">
            <p className="text-center text-xs text-text-tertiary">
              {data.length} active rule{data.length !== 1 ? 's' : ''}
            </p>
          </div>
        )}
      </Card>

      <UnmuteDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        suppressionId={deleteTarget?.id ?? null}
        ruleName={deleteTarget?.rule_name ?? undefined}
        onSuccess={handleDeleteSuccess}
      />

      <MutePanel
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        defaultFilters={{}}
        onSuccess={handleCreateSuccess}
      />
    </>
  )
}

// ─── Retention Section ───────────────────────────────────────────────────────

const DEFAULT_RETENTION = 180

function RetentionSection() {
  const activeTenantId = useAuthStore((s) => s.activeTenantId())
  const homeTenantId = useAuthStore((s) => s.homeTenantId())
  const tenantId = activeTenantId ?? homeTenantId

  // FIX-229 Gate F-U3: track the raw input string separately from the parsed
  // numeric value so empty input gets a "Required" error, not a misleading
  // "Must be between 30 and 365".
  const [rawValue, setRawValue] = useState<string>(String(DEFAULT_RETENTION))
  const [inputError, setInputError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [loaded, setLoaded] = useState(false)

  // Fetch current tenant settings
  const { isLoading: tenantLoading } = useQuery({
    queryKey: ['tenant-self', tenantId],
    queryFn: async () => {
      if (!tenantId) return null
      const res = await api.get<{ status: string; data: TenantResponse }>(`/tenants/${tenantId}`)
      const settings = res.data.data?.settings
      if (settings && typeof settings === 'object' && 'alert_retention_days' in settings) {
        const days = (settings as TenantSettings).alert_retention_days
        if (typeof days === 'number') {
          setRawValue(String(days))
        }
      }
      setLoaded(true)
      return res.data.data
    },
    enabled: !!tenantId,
    staleTime: 60_000,
  })

  const handleSave = async () => {
    if (!tenantId) return
    if (!rawValue.trim()) {
      setInputError('Required')
      return
    }
    const parsed = Number(rawValue)
    if (!Number.isInteger(parsed) || parsed < 30 || parsed > 365) {
      setInputError('Must be between 30 and 365')
      return
    }
    setInputError(null)
    setSaving(true)
    try {
      // Fetch existing settings first to merge (avoid overwriting other keys)
      let existingSettings: TenantSettings = {}
      try {
        const existing = await api.get<{ status: string; data: TenantResponse }>(`/tenants/${tenantId}`)
        if (existing.data.data?.settings && typeof existing.data.data.settings === 'object') {
          existingSettings = existing.data.data.settings as TenantSettings
        }
      } catch {
        // proceed with empty — will only set the one key
      }

      const merged = { ...existingSettings, alert_retention_days: parsed }
      await api.patch(`/tenants/${tenantId}`, { settings: merged })
      toast.success('Alert retention updated')
    } catch (err: unknown) {
      if (axios.isAxiosError(err)) {
        const msg = err.response?.data?.error?.message
        if (err.response?.status === 422 || err.response?.status === 400) {
          setInputError(msg ?? 'Must be between 30 and 365')
        } else {
          toast.error(msg ?? 'Failed to update retention')
        }
      } else {
        toast.error('Failed to update retention')
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card>
      <CardContent className="p-5 space-y-4">
        <div>
          <h3 className="text-sm font-medium text-text-primary mb-0.5">Alert Retention</h3>
          <p className="text-xs text-text-secondary">
            How long to keep alert history before automatic deletion.
          </p>
        </div>

        <div className="flex flex-col gap-1 max-w-xs">
          <label
            htmlFor="retention-days"
            className="text-[11px] uppercase tracking-[1.5px] text-text-tertiary"
          >
            Days (30 – 365)
          </label>
          {tenantLoading && !loaded ? (
            <Skeleton className="h-9 w-full" />
          ) : (
            <Input
              id="retention-days"
              type="number"
              min={30}
              max={365}
              step={1}
              value={rawValue}
              onChange={(e) => {
                setRawValue(e.target.value)
                if (inputError) setInputError(null)
              }}
              aria-invalid={!!inputError}
              className="w-full"
            />
          )}
          {inputError && (
            <p className="text-[11px] text-danger">{inputError}</p>
          )}
        </div>

        <Button
          onClick={handleSave}
          disabled={saving || tenantLoading}
          size="sm"
          className="gap-1.5"
        >
          {saving ? (
            <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent" />
          ) : null}
          Save
        </Button>
      </CardContent>
    </Card>
  )
}

// ─── Page ────────────────────────────────────────────────────────────────────

export default function AlertRulesPage() {
  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between mb-2">
        <div>
          <p className="text-[11px] text-text-tertiary mb-1">Settings / Alert Rules</p>
          <h1 className="text-[16px] font-semibold text-text-primary">Alert Rules</h1>
          <p className="text-xs text-text-secondary mt-1">
            Manage suppression rules and retention.
          </p>
        </div>
      </div>

      {/* Active suppression rules */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <BellOff className="h-4 w-4 text-text-tertiary" />
          <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary">
            Suppression Rules
          </h2>
        </div>
        <ActiveRulesSection />
      </div>

      {/* Retention */}
      <div className="space-y-3">
        <div className="flex items-center gap-2">
          <AlertCircle className="h-4 w-4 text-text-tertiary" />
          <h2 className="text-xs font-medium uppercase tracking-[1.5px] text-text-tertiary">
            Retention
          </h2>
        </div>
        <RetentionSection />
      </div>
    </div>
  )
}
