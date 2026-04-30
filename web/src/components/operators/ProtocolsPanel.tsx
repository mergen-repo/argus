import { useState, useMemo, useEffect, useRef } from 'react'
import { toast } from 'sonner'
import { CheckCircle2, XOctagon, Loader2, Zap, Plus, Trash2, Radio } from 'lucide-react'
import { Card, CardHeader, CardContent, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { useUpdateOperator } from '@/hooks/use-operators'
import { api } from '@/lib/api'
import type {
  AdapterConfig,
  AdapterProtocolConfig,
  Operator,
  OperatorTestResult,
} from '@/types/operator'
import { cn } from '@/lib/utils'

const PROTOCOL_ORDER: Array<keyof AdapterConfig> = [
  'radius',
  'diameter',
  'sba',
  'http',
  'mock',
]

const PROTOCOL_LABEL: Record<string, string> = {
  mock: 'Mock',
  radius: 'RADIUS',
  diameter: 'Diameter',
  sba: '5G SBA',
  http: 'HTTP',
}

const PROTOCOL_DESCRIPTION: Record<string, string> = {
  mock: 'Deterministic in-process adapter for staging and regression tests.',
  radius: 'RFC 2865/5997 authentication + accounting over UDP.',
  diameter: 'RFC 6733 Gx / Gy credit-control + authorization.',
  sba: '3GPP 5G Service-Based Architecture (NRF heartbeat).',
  http: 'Generic HTTP gateway with pluggable auth.',
}

const AUTH_TYPE_OPTIONS = [
  { value: 'none', label: 'None' },
  { value: 'bearer', label: 'Bearer token' },
  { value: 'basic', label: 'Basic auth' },
  { value: 'mtls', label: 'mTLS' },
]

function emptyConfig(): AdapterConfig {
  return {}
}

function cloneConfig(cfg: AdapterConfig | undefined): AdapterConfig {
  return JSON.parse(JSON.stringify(cfg ?? {})) as AdapterConfig
}

function FieldLabel({ children }: { children: React.ReactNode }) {
  return (
    <label className="text-xs font-medium text-text-secondary mb-1.5 block">
      {children}
    </label>
  )
}

type TestState = {
  loading: boolean
  result: OperatorTestResult | null
  error: string | null
}

function ProbeChip({ test }: { test: TestState }) {
  if (test.loading) {
    return (
      <Badge variant="secondary" className="gap-1">
        <Loader2 className="h-3 w-3 animate-spin" />
        Testing
      </Badge>
    )
  }
  if (test.error) {
    return (
      <Badge variant="danger" className="gap-1">
        <XOctagon className="h-3 w-3" />
        Failed
      </Badge>
    )
  }
  if (test.result) {
    if (test.result.success) {
      return (
        <Badge variant="success" className="gap-1">
          <CheckCircle2 className="h-3 w-3" />
          {test.result.latency_ms}ms
        </Badge>
      )
    }
    return (
      <Badge variant="danger" className="gap-1">
        <XOctagon className="h-3 w-3" />
        {test.result.latency_ms}ms
      </Badge>
    )
  }
  return null
}

function RadiusFields({
  cfg,
  onChange,
}: {
  cfg: AdapterProtocolConfig
  onChange: (next: AdapterProtocolConfig) => void
}) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
      <div>
        <FieldLabel>Shared Secret</FieldLabel>
        <Input
          type="password"
          placeholder="******"
          value={cfg.shared_secret ?? ''}
          onChange={(e) => onChange({ ...cfg, shared_secret: e.target.value })}
          className="h-8 text-sm font-mono"
          aria-label="RADIUS shared secret"
        />
      </div>
      <div>
        <FieldLabel>Listen Address</FieldLabel>
        <Input
          placeholder=":1812"
          value={cfg.listen_addr ?? ''}
          onChange={(e) => onChange({ ...cfg, listen_addr: e.target.value })}
          className="h-8 text-sm font-mono"
          aria-label="RADIUS listen address"
        />
      </div>
      <div>
        <FieldLabel>Accounting Port (optional)</FieldLabel>
        <Input
          placeholder=":1813"
          value={cfg.acct_port !== undefined ? String(cfg.acct_port) : ''}
          onChange={(e) => onChange({ ...cfg, acct_port: e.target.value })}
          className="h-8 text-sm font-mono"
          aria-label="RADIUS accounting port"
        />
      </div>
    </div>
  )
}

function DiameterFields({
  cfg,
  onChange,
}: {
  cfg: AdapterProtocolConfig
  onChange: (next: AdapterProtocolConfig) => void
}) {
  const peers = cfg.peers ?? []
  const updatePeer = (idx: number, value: string) => {
    const next = [...peers]
    next[idx] = value
    onChange({ ...cfg, peers: next })
  }
  const addPeer = () => onChange({ ...cfg, peers: [...peers, ''] })
  const removePeer = (idx: number) =>
    onChange({ ...cfg, peers: peers.filter((_, i) => i !== idx) })

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div>
          <FieldLabel>Origin Host</FieldLabel>
          <Input
            placeholder="argus.local"
            value={cfg.origin_host ?? ''}
            onChange={(e) => onChange({ ...cfg, origin_host: e.target.value })}
            className="h-8 text-sm font-mono"
          />
        </div>
        <div>
          <FieldLabel>Origin Realm</FieldLabel>
          <Input
            placeholder="argus.local"
            value={cfg.origin_realm ?? ''}
            onChange={(e) => onChange({ ...cfg, origin_realm: e.target.value })}
            className="h-8 text-sm font-mono"
          />
        </div>
      </div>
      <div>
        <FieldLabel>Product Name (optional)</FieldLabel>
        <Input
          placeholder="Argus"
          value={cfg.product_name ?? ''}
          onChange={(e) => onChange({ ...cfg, product_name: e.target.value })}
          className="h-8 text-sm"
        />
      </div>
      <div>
        <FieldLabel>Peers</FieldLabel>
        <div className="space-y-2">
          {peers.length === 0 && (
            <p className="text-xs text-text-tertiary">
              No peers configured. Add at least one host:port entry.
            </p>
          )}
          {peers.map((peer, idx) => (
            <div key={idx} className="flex items-center gap-2">
              <Input
                placeholder="10.0.1.10:3868"
                value={peer}
                onChange={(e) => updatePeer(idx, e.target.value)}
                className="h-8 text-sm font-mono"
              />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-8 w-8 text-text-tertiary hover:text-danger"
                onClick={() => removePeer(idx)}
                aria-label={`Remove peer ${idx + 1}`}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="gap-1.5 h-8"
            onClick={addPeer}
          >
            <Plus className="h-3.5 w-3.5" />
            Add peer
          </Button>
        </div>
      </div>
    </div>
  )
}

function SbaFields({
  cfg,
  onChange,
}: {
  cfg: AdapterProtocolConfig
  onChange: (next: AdapterProtocolConfig) => void
}) {
  const tls = cfg.tls ?? {}
  const updateTls = (patch: AdapterProtocolConfig['tls']) =>
    onChange({ ...cfg, tls: { ...tls, ...patch } })

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div>
          <FieldLabel>NRF URL</FieldLabel>
          <Input
            placeholder="https://nrf.5gc.example/nnrf-nfm/v1"
            value={cfg.nrf_url ?? ''}
            onChange={(e) => onChange({ ...cfg, nrf_url: e.target.value })}
            className="h-8 text-sm font-mono"
          />
        </div>
        <div>
          <FieldLabel>NF Instance ID (optional)</FieldLabel>
          <Input
            placeholder="UUID"
            value={cfg.nf_instance_id ?? ''}
            onChange={(e) => onChange({ ...cfg, nf_instance_id: e.target.value })}
            className="h-8 text-sm font-mono"
          />
        </div>
      </div>
      <details className="group">
        <summary className="text-xs font-medium text-text-secondary cursor-pointer select-none">
          TLS configuration (optional)
        </summary>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 mt-3 pl-3 border-l border-border">
          <div>
            <FieldLabel>Cert path</FieldLabel>
            <Input
              placeholder="/etc/argus/tls/cert.pem"
              value={tls.cert_path ?? ''}
              onChange={(e) => updateTls({ cert_path: e.target.value })}
              className="h-8 text-sm font-mono"
            />
          </div>
          <div>
            <FieldLabel>Key path</FieldLabel>
            <Input
              placeholder="/etc/argus/tls/key.pem"
              value={tls.key_path ?? ''}
              onChange={(e) => updateTls({ key_path: e.target.value })}
              className="h-8 text-sm font-mono"
            />
          </div>
          <div>
            <FieldLabel>CA path</FieldLabel>
            <Input
              placeholder="/etc/argus/tls/ca.pem"
              value={tls.ca_path ?? ''}
              onChange={(e) => updateTls({ ca_path: e.target.value })}
              className="h-8 text-sm font-mono"
            />
          </div>
        </div>
      </details>
    </div>
  )
}

function HttpFields({
  cfg,
  onChange,
}: {
  cfg: AdapterProtocolConfig
  onChange: (next: AdapterProtocolConfig) => void
}) {
  const authType = cfg.auth_type ?? 'none'
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div>
          <FieldLabel>Base URL</FieldLabel>
          <Input
            placeholder="https://api.operator.example"
            value={cfg.base_url ?? ''}
            onChange={(e) => onChange({ ...cfg, base_url: e.target.value })}
            className="h-8 text-sm font-mono"
          />
        </div>
        <div>
          <FieldLabel>Health Path (optional)</FieldLabel>
          <Input
            placeholder="/health"
            value={cfg.health_path ?? ''}
            onChange={(e) => onChange({ ...cfg, health_path: e.target.value })}
            className="h-8 text-sm font-mono"
          />
        </div>
      </div>
      <div>
        <FieldLabel>Authentication</FieldLabel>
        <Select
          value={authType}
          onChange={(e) => onChange({ ...cfg, auth_type: e.target.value })}
          className="h-8 text-sm"
          options={AUTH_TYPE_OPTIONS}
        />
      </div>
      {authType === 'bearer' && (
        <div>
          <FieldLabel>Bearer Token</FieldLabel>
          <Input
            type="password"
            placeholder="******"
            value={cfg.auth_token ?? ''}
            onChange={(e) => onChange({ ...cfg, auth_token: e.target.value })}
            className="h-8 text-sm font-mono"
          />
        </div>
      )}
      {authType === 'basic' && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          <div>
            <FieldLabel>Username</FieldLabel>
            <Input
              value={cfg.username ?? ''}
              onChange={(e) => onChange({ ...cfg, username: e.target.value })}
              className="h-8 text-sm font-mono"
            />
          </div>
          <div>
            <FieldLabel>Password</FieldLabel>
            <Input
              type="password"
              placeholder="******"
              value={cfg.password ?? ''}
              onChange={(e) => onChange({ ...cfg, password: e.target.value })}
              className="h-8 text-sm font-mono"
            />
          </div>
        </div>
      )}
    </div>
  )
}

function MockFields({
  cfg,
  onChange,
}: {
  cfg: AdapterProtocolConfig
  onChange: (next: AdapterProtocolConfig) => void
}) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
      <div>
        <FieldLabel>Simulated latency (ms)</FieldLabel>
        <Input
          type="number"
          min={0}
          placeholder="10"
          value={cfg.latency_ms !== undefined ? String(cfg.latency_ms) : ''}
          onChange={(e) =>
            onChange({
              ...cfg,
              latency_ms: e.target.value === '' ? undefined : Number(e.target.value),
            })
          }
          className="h-8 text-sm font-mono"
        />
      </div>
      <div>
        <FieldLabel>Simulated IMSI count (optional)</FieldLabel>
        <Input
          type="number"
          min={0}
          placeholder="1000"
          value={cfg.simulated_imsi_count !== undefined ? String(cfg.simulated_imsi_count) : ''}
          onChange={(e) =>
            onChange({
              ...cfg,
              simulated_imsi_count: e.target.value === '' ? undefined : Number(e.target.value),
            })
          }
          className="h-8 text-sm font-mono"
        />
      </div>
    </div>
  )
}

function ProtocolFields({
  protocol,
  cfg,
  onChange,
}: {
  protocol: string
  cfg: AdapterProtocolConfig
  onChange: (next: AdapterProtocolConfig) => void
}) {
  switch (protocol) {
    case 'radius':
      return <RadiusFields cfg={cfg} onChange={onChange} />
    case 'diameter':
      return <DiameterFields cfg={cfg} onChange={onChange} />
    case 'sba':
      return <SbaFields cfg={cfg} onChange={onChange} />
    case 'http':
      return <HttpFields cfg={cfg} onChange={onChange} />
    case 'mock':
      return <MockFields cfg={cfg} onChange={onChange} />
    default:
      return null
  }
}

function ProtocolCard({
  protocol,
  cfg,
  dirty,
  test,
  onToggle,
  onChange,
  onTest,
}: {
  protocol: string
  cfg: AdapterProtocolConfig
  dirty: boolean
  test: TestState
  onToggle: (next: boolean) => void
  onChange: (next: AdapterProtocolConfig) => void
  onTest: () => void
}) {
  const enabled = cfg.enabled === true

  return (
    <Card className={cn('transition-colors', enabled ? 'border-accent/40' : 'border-border')}>
      <CardHeader className="flex flex-row items-start justify-between gap-3 pb-3">
        <div className="flex items-start gap-3 min-w-0">
          <div
            className={cn(
              'mt-0.5 flex h-8 w-8 items-center justify-center rounded-[var(--radius-sm)]',
              enabled ? 'bg-accent-dim text-accent' : 'bg-bg-elevated text-text-tertiary',
            )}
          >
            <Radio className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <CardTitle className="text-sm font-semibold text-text-primary">
              {PROTOCOL_LABEL[protocol] ?? protocol}
            </CardTitle>
            <p className="text-xs text-text-secondary mt-1">
              {PROTOCOL_DESCRIPTION[protocol] ?? ''}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          <ProbeChip test={test} />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onToggle(!enabled)}
            className={cn(
              'h-7 px-2.5 text-xs font-mono border transition-colors',
              enabled
                ? 'border-accent bg-accent-dim text-accent hover:bg-accent-dim hover:text-accent'
                : 'border-border bg-bg-elevated text-text-secondary hover:border-text-tertiary',
            )}
            role="switch"
            aria-checked={enabled}
            aria-label={`${PROTOCOL_LABEL[protocol]} enabled`}
          >
            {enabled ? 'ENABLED' : 'DISABLED'}
          </Button>
        </div>
      </CardHeader>
      {enabled && (
        <CardContent className="space-y-4 pt-0">
          <ProtocolFields protocol={protocol} cfg={cfg} onChange={onChange} />
          <div className="flex items-center justify-between border-t border-border pt-3">
            <p className="text-xs text-text-tertiary">
              {test.error
                ? test.error
                : test.result?.success
                  ? `Last probe: ${test.result.latency_ms}ms · OK`
                  : test.result && !test.result.success
                    ? `Last probe: ${test.result.latency_ms}ms · ${test.result.error ?? 'FAILED'}`
                    : 'No probe run yet. Save the card, then Test Connection.'}
            </p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={onTest}
              disabled={dirty || test.loading}
              className="gap-1.5 h-8"
            >
              {test.loading ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Zap className="h-3.5 w-3.5" />
              )}
              Test Connection
            </Button>
          </div>
        </CardContent>
      )}
    </Card>
  )
}

export function ProtocolsPanel({ operator }: { operator: Operator }) {
  const [draft, setDraft] = useState<AdapterConfig>(() => cloneConfig(operator.adapter_config ?? emptyConfig()))
  const [initial, setInitial] = useState<AdapterConfig>(() => cloneConfig(operator.adapter_config ?? emptyConfig()))
  const [testStates, setTestStates] = useState<Record<string, TestState>>({})

  const updateMutation = useUpdateOperator(operator.id)

  const dirty = useMemo(() => {
    return JSON.stringify(draft) !== JSON.stringify(initial)
  }, [draft, initial])

  // STORY-090 Gate (F-A2): sync local state when the server returns a
  // fresher adapter_config (first detail fetch, post-save refetch,
  // WebSocket-triggered invalidation). Skip when the user has dirty
  // edits to avoid clobbering in-progress work.
  const serverConfigKey = JSON.stringify(operator.adapter_config ?? null)
  const lastSyncedRef = useRef<string>(serverConfigKey)
  useEffect(() => {
    if (dirty) return
    if (lastSyncedRef.current === serverConfigKey) return
    const next = cloneConfig(operator.adapter_config ?? emptyConfig())
    setDraft(next)
    setInitial(cloneConfig(next))
    lastSyncedRef.current = serverConfigKey
  }, [serverConfigKey, dirty, operator.adapter_config])

  const handleToggle = (protocol: string, enabled: boolean) => {
    setDraft((prev) => ({
      ...prev,
      [protocol]: { ...(prev[protocol as keyof AdapterConfig] ?? {}), enabled },
    }))
  }

  const handleChange = (protocol: string, next: AdapterProtocolConfig) => {
    setDraft((prev) => ({
      ...prev,
      [protocol]: next,
    }))
  }

  const runTest = async (protocol: string) => {
    setTestStates((prev) => ({
      ...prev,
      [protocol]: { loading: true, result: null, error: null },
    }))
    try {
      const res = await api.post<{ data: OperatorTestResult }>(
        `/operators/${operator.id}/test/${protocol}`,
      )
      setTestStates((prev) => ({
        ...prev,
        [protocol]: { loading: false, result: res.data.data, error: null },
      }))
      if (res.data.data.success) {
        toast.success(`${PROTOCOL_LABEL[protocol]}: connection OK (${res.data.data.latency_ms}ms)`)
      } else {
        toast.error(`${PROTOCOL_LABEL[protocol]}: ${res.data.data.error ?? 'probe failed'}`)
      }
    } catch (err) {
      const msg =
        (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message ??
        'Test failed'
      setTestStates((prev) => ({
        ...prev,
        [protocol]: { loading: false, result: null, error: msg },
      }))
      toast.error(`${PROTOCOL_LABEL[protocol]}: ${msg}`)
    }
  }

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync({ adapter_config: draft })
      setInitial(cloneConfig(draft))
      toast.success('Protocols saved')
    } catch (err) {
      const msg =
        (err as { response?: { data?: { error?: { message?: string } } } })?.response?.data?.error?.message ??
        'Failed to save protocols'
      toast.error(msg)
    }
  }

  const handleRevert = () => {
    setDraft(cloneConfig(initial))
  }

  const enabledCount = PROTOCOL_ORDER.filter(
    (p) => draft[p]?.enabled === true,
  ).length

  return (
    <div className="space-y-4 pt-2">
      <Card>
        <CardContent className="flex flex-col gap-2 pt-4 md:flex-row md:items-center md:justify-between">
          <div>
            <h3 className="text-sm font-semibold text-text-primary">Protocol configuration</h3>
            <p className="text-xs text-text-secondary mt-1">
              Enable one or more protocols, configure their transport, and run a per-protocol Test Connection.
              {enabledCount > 0 && (
                <>
                  {' '}
                  <span className="font-mono text-accent">{enabledCount}</span> enabled.
                </>
              )}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={!dirty || updateMutation.isPending}
              onClick={handleRevert}
            >
              Revert
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={!dirty || updateMutation.isPending}
              onClick={handleSave}
              className="gap-1.5"
            >
              {updateMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Save Protocols
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-3">
        {PROTOCOL_ORDER.map((protocol) => {
          const cfg = draft[protocol] ?? {}
          const test = testStates[protocol] ?? { loading: false, result: null, error: null }
          return (
            <ProtocolCard
              key={protocol}
              protocol={protocol}
              cfg={cfg}
              dirty={dirty}
              test={test}
              onToggle={(enabled) => handleToggle(protocol, enabled)}
              onChange={(next) => handleChange(protocol, next)}
              onTest={() => runTest(protocol)}
            />
          )
        })}
      </div>
    </div>
  )
}

export default ProtocolsPanel
