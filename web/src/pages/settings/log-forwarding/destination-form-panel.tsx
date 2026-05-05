// STORY-098 Task 6 — Add/Edit slide-panel for syslog destinations.
// Option C: rich form lives in a SlidePanel; Delete uses a separate compact
// Dialog (in index.tsx). TLS group reveals on transport=tls. Test Connection
// posts the current draft to /settings/log-forwarding/test (no DB write).

import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { AlertCircle, Loader2, Lock, RadioTower, ShieldCheck, Wifi, Network as NetworkIcon, Power, FlaskConical } from 'lucide-react'
import { SlidePanel, SlidePanelFooter } from '@/components/ui/slide-panel'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import {
  type DestinationFormDraft,
  type DestinationFormErrors,
  type SyslogCategory,
  type SyslogDestination,
  type SyslogFormat,
  type SyslogTransport,
  INITIAL_DESTINATION_FORM,
  SYSLOG_CATEGORIES,
  SYSLOG_CATEGORY_LABEL,
  SYSLOG_FACILITIES,
  SYSLOG_SEVERITIES,
  destinationToForm,
  formToUpsertRequest,
  requiresTLSGroup,
  validateDestinationForm,
} from '@/types/log-forwarding'
import { useLogForwardingTest, useLogForwardingUpsert } from '@/hooks/use-log-forwarding'
import { TestResultBanner, type TestResultState } from './test-result-banner'

interface DestinationFormPanelProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  editing: SyslogDestination | null
}

interface ApiErrorShape {
  response?: {
    status?: number
    data?: { error?: { code?: string; message?: string } }
  }
}

const TRANSPORT_META: Record<SyslogTransport, { icon: React.ElementType; description: string }> = {
  udp: { icon: Wifi, description: 'Fire-and-forget. No delivery guarantee.' },
  tcp: { icon: NetworkIcon, description: 'Reliable framing (RFC 6587). Auto-reconnect.' },
  tls: { icon: Lock, description: 'TCP wrapped in TLS 1.2+ with hostname verify.' },
}

const FORMAT_META: Record<SyslogFormat, { description: string }> = {
  rfc3164: { description: 'BSD legacy. ASCII-only, line-delimited.' },
  rfc5424: { description: 'Modern. Structured data + UTF-8 BOM.' },
}

export function DestinationFormPanel({ open, onOpenChange, editing }: DestinationFormPanelProps) {
  const [form, setForm] = useState<DestinationFormDraft>(INITIAL_DESTINATION_FORM)
  const [errors, setErrors] = useState<DestinationFormErrors>({})
  const [testResult, setTestResult] = useState<TestResultState>({ state: 'idle' })

  const upsert = useLogForwardingUpsert()
  const test = useLogForwardingTest()

  useEffect(() => {
    if (!open) return
    setForm(editing ? destinationToForm(editing) : INITIAL_DESTINATION_FORM)
    setErrors({})
    setTestResult({ state: 'idle' })
  }, [open, editing])

  function setField<K extends keyof DestinationFormDraft>(key: K, value: DestinationFormDraft[K]) {
    setForm((f) => ({ ...f, [key]: value }))
    setErrors((e) => {
      if (!e[key]) return e
      const next = { ...e }
      delete next[key]
      return next
    })
    setTestResult({ state: 'idle' })
  }

  function toggleCategory(cat: SyslogCategory) {
    setForm((f) => {
      const exists = f.filter_categories.includes(cat)
      return {
        ...f,
        filter_categories: exists
          ? f.filter_categories.filter((c) => c !== cat)
          : [...f.filter_categories, cat],
      }
    })
    setErrors((e) => {
      if (!e.filter_categories) return e
      const next = { ...e }
      delete next.filter_categories
      return next
    })
  }

  function toggleAllCategories() {
    setForm((f) => ({
      ...f,
      filter_categories:
        f.filter_categories.length === SYSLOG_CATEGORIES.length ? [] : [...SYSLOG_CATEGORIES],
    }))
  }

  async function handleTest() {
    const validation = validateDestinationForm(form)
    if (Object.keys(validation).length > 0) {
      setErrors(validation)
      setTestResult({ state: 'idle' })
      return
    }
    setTestResult({ state: 'pending' })
    try {
      const result = await test.mutateAsync(formToUpsertRequest(form))
      if (result.ok) {
        setTestResult({ state: 'ok' })
      } else {
        setTestResult({ state: 'error', message: result.error ?? 'Unknown error' })
      }
    } catch (err) {
      const e = err as ApiErrorShape
      const msg = e.response?.data?.error?.message ?? 'Test request failed'
      setTestResult({ state: 'error', message: msg })
    }
  }

  async function handleSave() {
    const validation = validateDestinationForm(form)
    if (Object.keys(validation).length > 0) {
      setErrors(validation)
      return
    }
    try {
      await upsert.mutateAsync(formToUpsertRequest(form))
      toast.success(editing ? 'Destination updated' : 'Destination saved')
      onOpenChange(false)
    } catch (err) {
      const e = err as ApiErrorShape
      const code = e.response?.data?.error?.code
      const msg = e.response?.data?.error?.message ?? 'Save failed'
      // Map known 422 codes to inline field errors.
      if (code === 'INVALID_TRANSPORT') setErrors((x) => ({ ...x, transport: msg }))
      else if (code === 'INVALID_FORMAT') setErrors((x) => ({ ...x, format: msg }))
      else if (code === 'INVALID_FACILITY') setErrors((x) => ({ ...x, facility: msg }))
      else if (code === 'INVALID_CATEGORY') setErrors((x) => ({ ...x, filter_categories: msg }))
      else if (code === 'TLS_CONFIG_INVALID') setErrors((x) => ({ ...x, tls_pair: msg }))
      // generic toast already surfaced by api interceptor
    }
  }

  const showTLSGroup = requiresTLSGroup(form.transport)
  const allChecked = form.filter_categories.length === SYSLOG_CATEGORIES.length

  return (
    <SlidePanel
      open={open}
      onOpenChange={onOpenChange}
      title={editing ? 'Edit Destination' : 'Add Destination'}
      description={
        editing
          ? `Update settings for "${editing.name}".`
          : 'Forward Argus events to your SIEM via RFC 3164 / RFC 5424.'
      }
      width="md"
    >
      <div className="space-y-6 pb-6">
        {/* ── Identity section ─────────────────────────────────── */}
        <Section title="Identity" icon={RadioTower}>
          <Field label="Name" htmlFor="lf-name" error={errors.name} required>
            <Input
              id="lf-name"
              value={form.name}
              onChange={(e) => setField('name', e.target.value)}
              placeholder="e.g. siem-prod"
              maxLength={255}
              autoComplete="off"
            />
          </Field>
          <div className="grid grid-cols-3 gap-3">
            <div className="col-span-2">
              <Field label="Host" htmlFor="lf-host" error={errors.host} required>
                <Input
                  id="lf-host"
                  value={form.host}
                  onChange={(e) => setField('host', e.target.value)}
                  placeholder="splunk.corp.example.net"
                  maxLength={255}
                  autoComplete="off"
                />
              </Field>
            </div>
            <Field label="Port" htmlFor="lf-port" error={errors.port} required>
              <Input
                id="lf-port"
                type="number"
                min={1}
                max={65535}
                value={form.port}
                onChange={(e) => setField('port', Number.parseInt(e.target.value, 10) || 0)}
              />
            </Field>
          </div>
        </Section>

        {/* ── Transport section ────────────────────────────────── */}
        <Section title="Transport & Format" icon={NetworkIcon}>
          <div>
            <p className="text-xs text-text-secondary mb-2">Transport</p>
            <div className="grid grid-cols-3 gap-2">
              {(['udp', 'tcp', 'tls'] as const).map((t) => {
                const meta = TRANSPORT_META[t]
                const TIcon = meta.icon
                const active = form.transport === t
                return (
                  <button
                    key={t}
                    type="button"
                    onClick={() => setField('transport', t)}
                    className={cn(
                      'flex flex-col items-start gap-1 rounded-[var(--radius-sm)] border px-3 py-2.5 text-left transition-colors',
                      active
                        ? t === 'udp'
                          ? 'border-text-tertiary bg-bg-elevated text-text-primary'
                          : t === 'tcp'
                            ? 'border-accent/40 bg-accent-dim text-accent'
                            : 'border-success/40 bg-success-dim text-success'
                        : 'border-border bg-bg-surface text-text-secondary hover:border-text-tertiary',
                    )}
                  >
                    <span className="flex items-center gap-1.5 font-medium uppercase tracking-wide text-[11px]">
                      <TIcon className="h-3.5 w-3.5" />
                      {t}
                    </span>
                    <span className="text-[10px] leading-snug opacity-80">{meta.description}</span>
                  </button>
                )
              })}
            </div>
            {errors.transport && <FieldError msg={errors.transport} />}
          </div>

          <div>
            <p className="text-xs text-text-secondary mb-2">Format</p>
            <div className="grid grid-cols-2 gap-2">
              {(['rfc3164', 'rfc5424'] as const).map((f) => {
                const active = form.format === f
                return (
                  <button
                    key={f}
                    type="button"
                    onClick={() => setField('format', f)}
                    className={cn(
                      'flex flex-col items-start gap-1 rounded-[var(--radius-sm)] border px-3 py-2.5 text-left transition-colors',
                      active
                        ? f === 'rfc3164'
                          ? 'border-text-tertiary bg-bg-elevated text-text-primary'
                          : 'border-accent/40 bg-accent-dim text-accent'
                        : 'border-border bg-bg-surface text-text-secondary hover:border-text-tertiary',
                    )}
                  >
                    <span className="font-mono uppercase tracking-wide text-[11px] font-medium">
                      {f.toUpperCase()}
                    </span>
                    <span className="text-[10px] leading-snug opacity-80">
                      {FORMAT_META[f].description}
                    </span>
                  </button>
                )
              })}
            </div>
            {errors.format && <FieldError msg={errors.format} />}
          </div>

          <div className="grid grid-cols-2 gap-3">
            <Field label="Facility" htmlFor="lf-facility" error={errors.facility}>
              <Select
                id="lf-facility"
                value={String(form.facility)}
                onChange={(e) => setField('facility', Number.parseInt(e.target.value, 10))}
                options={SYSLOG_FACILITIES.map((f) => ({ value: String(f.value), label: f.label }))}
              />
            </Field>
            <Field
              label="Severity floor (optional)"
              htmlFor="lf-severity-floor"
              error={errors.severity_floor}
            >
              <Select
                id="lf-severity-floor"
                value={form.severity_floor === null ? '' : String(form.severity_floor)}
                onChange={(e) =>
                  setField(
                    'severity_floor',
                    e.target.value === '' ? null : Number.parseInt(e.target.value, 10),
                  )
                }
                options={[
                  { value: '', label: 'No floor' },
                  ...SYSLOG_SEVERITIES.map((s) => ({ value: String(s.value), label: s.label })),
                ]}
              />
            </Field>
          </div>
        </Section>

        {/* ── Filter section ───────────────────────────────────── */}
        <Section title="Filter Categories" icon={ShieldCheck}>
          <div>
            <div className="flex items-center justify-between mb-2">
              <p className="text-xs text-text-secondary">
                Forward only events in these categories.
              </p>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={toggleAllCategories}
                className="h-auto p-0 text-[11px] text-accent hover:text-accent/80 hover:bg-transparent"
              >
                {allChecked ? 'Clear all' : 'Select all'}
              </Button>
            </div>
            <div className="grid grid-cols-2 gap-1.5">
              {SYSLOG_CATEGORIES.map((cat) => {
                const checked = form.filter_categories.includes(cat)
                return (
                  <label
                    key={cat}
                    className={cn(
                      'flex items-center gap-2 px-2.5 py-1.5 rounded-[var(--radius-sm)] border text-xs cursor-pointer transition-colors',
                      checked
                        ? 'border-accent/40 bg-accent-dim text-accent'
                        : 'border-border bg-bg-surface text-text-secondary hover:border-text-tertiary',
                    )}
                  >
                    <input
                      type="checkbox"
                      className="sr-only"
                      checked={checked}
                      onChange={() => toggleCategory(cat)}
                      aria-label={`Forward ${SYSLOG_CATEGORY_LABEL[cat]} events`}
                    />
                    <span
                      className={cn(
                        'h-3.5 w-3.5 rounded-sm border flex items-center justify-center flex-shrink-0',
                        checked ? 'border-accent bg-accent' : 'border-border',
                      )}
                    >
                      {checked && (
                        <span className="h-1.5 w-1.5 rounded-sm bg-bg-surface" aria-hidden />
                      )}
                    </span>
                    {SYSLOG_CATEGORY_LABEL[cat]}
                  </label>
                )
              })}
            </div>
            {errors.filter_categories && <FieldError msg={errors.filter_categories} />}
          </div>

          <Field
            label="Minimum severity (optional)"
            htmlFor="lf-min-severity"
            error={errors.filter_min_severity}
          >
            <Select
              id="lf-min-severity"
              value={form.filter_min_severity === null ? '' : String(form.filter_min_severity)}
              onChange={(e) =>
                setField(
                  'filter_min_severity',
                  e.target.value === '' ? null : Number.parseInt(e.target.value, 10),
                )
              }
              options={[
                { value: '', label: 'No filter' },
                ...SYSLOG_SEVERITIES.map((s) => ({ value: String(s.value), label: s.label })),
              ]}
            />
          </Field>
        </Section>

        {/* ── TLS group (transport=tls only) ───────────────────── */}
        {showTLSGroup && (
          <Section
            title="TLS Material"
            icon={Lock}
            description="Provide a CA bundle to verify the server. Provide both client cert and key for mutual TLS, or leave both blank."
          >
            <Field label="CA bundle (PEM)" htmlFor="lf-ca-pem">
              <Textarea
                id="lf-ca-pem"
                value={form.tls_ca_pem}
                onChange={(e) => setField('tls_ca_pem', e.target.value)}
                placeholder="-----BEGIN CERTIFICATE-----"
                rows={4}
                className="font-mono text-[11px]"
                spellCheck={false}
              />
            </Field>
            <Field label="Client certificate (PEM)" htmlFor="lf-cert-pem">
              <Textarea
                id="lf-cert-pem"
                value={form.tls_client_cert_pem}
                onChange={(e) => setField('tls_client_cert_pem', e.target.value)}
                placeholder="-----BEGIN CERTIFICATE-----"
                rows={4}
                className="font-mono text-[11px]"
                spellCheck={false}
              />
            </Field>
            <Field label="Client private key (PEM)" htmlFor="lf-key-pem">
              <Textarea
                id="lf-key-pem"
                value={form.tls_client_key_pem}
                onChange={(e) => setField('tls_client_key_pem', e.target.value)}
                placeholder="-----BEGIN PRIVATE KEY-----"
                rows={4}
                className="font-mono text-[11px]"
                spellCheck={false}
              />
            </Field>
            {errors.tls_pair && (
              <div
                role="alert"
                className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-xs text-danger flex items-start gap-2"
              >
                <AlertCircle className="h-3.5 w-3.5 flex-shrink-0 mt-0.5" />
                <span>{errors.tls_pair}</span>
              </div>
            )}
          </Section>
        )}

        {/* ── Enabled toggle ───────────────────────────────────── */}
        <Section title="Activation" icon={Power}>
          <label className="flex items-center justify-between gap-3 rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-3 py-2.5">
            <span className="flex flex-col">
              <span className="text-sm font-medium text-text-primary">
                {form.enabled ? 'Enabled' : 'Disabled'}
              </span>
              <span className="text-[11px] text-text-tertiary">
                Disabled destinations are saved but receive no events.
              </span>
            </span>
            <input
              type="checkbox"
              className="sr-only"
              checked={form.enabled}
              onChange={(e) => setField('enabled', e.target.checked)}
              aria-label="Enabled"
            />
            <span
              className={cn(
                'relative h-5 w-9 rounded-full border transition-colors flex-shrink-0',
                form.enabled ? 'bg-accent border-accent' : 'bg-bg-surface border-border',
              )}
            >
              <span
                className={cn(
                  'absolute top-0.5 h-4 w-4 rounded-full bg-bg-surface shadow transition-transform',
                  form.enabled ? 'translate-x-4' : 'translate-x-0.5',
                )}
              />
            </span>
          </label>
        </Section>

        <TestResultBanner result={testResult} />
      </div>

      <SlidePanelFooter>
        <Button
          variant="outline"
          size="sm"
          onClick={handleTest}
          disabled={test.isPending || upsert.isPending}
          className="gap-2 mr-auto"
        >
          {test.isPending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <FlaskConical className="h-3.5 w-3.5" />
          )}
          Test Connection
        </Button>
        <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button
          size="sm"
          onClick={handleSave}
          disabled={upsert.isPending}
          className="gap-2"
        >
          {upsert.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          {editing ? 'Save Changes' : 'Save Destination'}
        </Button>
      </SlidePanelFooter>
    </SlidePanel>
  )
}

// ─── Local section + field helpers ──────────────────────────────────────────

function Section({
  title,
  icon: Icon,
  description,
  children,
}: {
  title: string
  icon: React.ElementType
  description?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <span className="flex h-6 w-6 items-center justify-center rounded-[var(--radius-sm)] bg-bg-elevated text-text-tertiary">
          <Icon className="h-3.5 w-3.5" />
        </span>
        <h3 className="text-[13px] font-semibold text-text-primary tracking-tight">{title}</h3>
      </div>
      {description && <p className="text-[11px] text-text-tertiary leading-relaxed">{description}</p>}
      <div className="space-y-3 pl-8">{children}</div>
    </div>
  )
}

function Field({
  label,
  htmlFor,
  error,
  required,
  children,
}: {
  label: string
  htmlFor: string
  error?: string
  required?: boolean
  children: React.ReactNode
}) {
  return (
    <div>
      <label htmlFor={htmlFor} className="text-xs text-text-secondary block mb-1.5">
        {label}
        {required && <span className="text-danger ml-0.5">*</span>}
      </label>
      {children}
      {error && <FieldError msg={error} />}
    </div>
  )
}

function FieldError({ msg }: { msg: string }) {
  return (
    <p role="alert" className="mt-1 text-xs text-danger flex items-center gap-1">
      <AlertCircle className="h-3 w-3 flex-shrink-0" />
      {msg}
    </p>
  )
}
