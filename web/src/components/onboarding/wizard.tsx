import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  getStoredSessionID,
  setStoredSessionID,
  useStartOnboarding,
  useSubmitOnboardingStep,
  useCompleteOnboarding,
  useOnboardingSession,
} from '@/hooks/use-onboarding'
import {
  Building2,
  Network,
  Globe,
  Smartphone,
  Shield,
  Check,
  ChevronLeft,
  ChevronRight,
  SkipForward,
  Upload,
  Zap,
  AlertCircle,
  CheckCircle2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Select } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { FileInput } from '@/components/ui/file-input'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import { useOperatorList } from '@/hooks/use-operators'
import { api } from '@/lib/api'
import type { ApiResponse } from '@/types/sim'

interface StepConfig {
  id: number
  title: string
  description: string
  icon: React.ElementType
  mandatory: boolean
}

const STEPS: StepConfig[] = [
  { id: 1, title: 'Tenant Profile', description: 'Set up your organization details', icon: Building2, mandatory: true },
  { id: 2, title: 'Operator Connection', description: 'Connect to your mobile operator', icon: Network, mandatory: true },
  { id: 3, title: 'APN Configuration', description: 'Create your first access point', icon: Globe, mandatory: true },
  { id: 4, title: 'SIM Import', description: 'Import your SIM cards', icon: Smartphone, mandatory: false },
  { id: 5, title: 'Policy Setup', description: 'Define your first policy', icon: Shield, mandatory: false },
]

const TIMEZONES = [
  { value: 'UTC', label: 'UTC' },
  { value: 'Europe/Istanbul', label: 'Europe/Istanbul (UTC+3)' },
  { value: 'Europe/London', label: 'Europe/London (UTC+0/+1)' },
  { value: 'Europe/Berlin', label: 'Europe/Berlin (UTC+1/+2)' },
  { value: 'America/New_York', label: 'America/New York (UTC-5/-4)' },
  { value: 'America/Los_Angeles', label: 'America/Los Angeles (UTC-8/-7)' },
  { value: 'Asia/Tokyo', label: 'Asia/Tokyo (UTC+9)' },
  { value: 'Asia/Dubai', label: 'Asia/Dubai (UTC+4)' },
]

const RETENTION_OPTIONS = [
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: '180', label: '180 days' },
  { value: '365', label: '1 year' },
]

const APN_TYPES = [
  { value: 'internet', label: 'Internet' },
  { value: 'iot', label: 'IoT' },
  { value: 'vpn', label: 'VPN' },
  { value: 'private', label: 'Private' },
]

const POLICY_TEMPLATES = [
  { value: 'basic-internet', label: 'Basic Internet Access', dsl: 'WHEN subscriber.state = "active"\nTHEN ALLOW\n  rate_limit = 10mbps\n  session_timeout = 86400' },
  { value: 'iot-restricted', label: 'IoT Restricted', dsl: 'WHEN subscriber.state = "active" AND apn.type = "iot"\nTHEN ALLOW\n  rate_limit = 1mbps\n  max_sessions = 1' },
  { value: 'blank', label: 'Blank (write your own)', dsl: '' },
]

function StepIndicator({ steps, currentStep, completedSteps }: { steps: StepConfig[]; currentStep: number; completedSteps: Set<number> }) {
  return (
    <div className="py-6">
      <div className="flex items-center justify-center">
        {steps.map((step, idx) => {
          const isCompleted = completedSteps.has(step.id)
          const isCurrent = step.id === currentStep
          const Icon = step.icon
          return (
            <div key={step.id} className="flex items-center">
              <div
                className={cn(
                  'flex h-10 w-10 shrink-0 items-center justify-center rounded-full border-2 transition-all duration-200',
                  isCompleted && 'border-success bg-success text-white',
                  isCurrent && !isCompleted && 'border-accent bg-accent-dim text-accent',
                  !isCurrent && !isCompleted && 'border-border bg-bg-surface text-text-tertiary',
                )}
              >
                {isCompleted ? <Check className="h-5 w-5" /> : <Icon className="h-5 w-5" />}
              </div>
              {idx < steps.length - 1 && (
                <div className={cn('mx-1.5 h-0.5 w-12 rounded-full transition-colors', isCompleted ? 'bg-success' : 'bg-border')} />
              )}
            </div>
          )
        })}
      </div>
      <div className="flex justify-center mt-2">
        {steps.map((step, idx) => {
          const isCurrent = step.id === currentStep
          return (
            <div key={step.id} className="flex items-center">
              <span
                className={cn(
                  'w-10 text-center text-[10px] leading-tight font-medium',
                  isCurrent ? 'text-text-primary' : 'text-text-tertiary',
                )}
              >
                {step.title.split(' ').map((w, i) => <span key={i} className="block">{w}</span>)}
              </span>
              {idx < steps.length - 1 && <div className="mx-1.5 w-12" />}
            </div>
          )
        })}
      </div>
    </div>
  )
}

function Step1TenantProfile({
  data,
  onChange,
}: {
  data: { companyName: string; timezone: string; retentionDays: string; contactEmail: string; locale: string }
  onChange: (data: { companyName: string; timezone: string; retentionDays: string; contactEmail: string; locale: string }) => void
}) {
  return (
    <div className="space-y-4">
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">Company Name *</label>
        <Input
          value={data.companyName}
          onChange={(e) => onChange({ ...data, companyName: e.target.value })}
          placeholder="Acme Corporation"
          autoFocus
        />
      </div>
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">Contact Email *</label>
        <Input
          type="email"
          value={data.contactEmail}
          onChange={(e) => onChange({ ...data, contactEmail: e.target.value })}
          placeholder="admin@company.com"
        />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1.5">
          <label className="block text-xs font-medium text-text-secondary">Language</label>
          <Select
            value={data.locale}
            onChange={(e) => onChange({ ...data, locale: e.target.value })}
            options={[{ value: 'en', label: 'English' }, { value: 'tr', label: 'Türkçe' }]}
          />
        </div>
        <div className="space-y-1.5">
          <label className="block text-xs font-medium text-text-secondary">Timezone</label>
          <Select
            value={data.timezone}
            onChange={(e) => onChange({ ...data, timezone: e.target.value })}
            options={TIMEZONES}
          />
        </div>
      </div>
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">Data Retention</label>
        <Select
          value={data.retentionDays}
          onChange={(e) => onChange({ ...data, retentionDays: e.target.value })}
          options={RETENTION_OPTIONS}
        />
      </div>
    </div>
  )
}

function Step2OperatorConnection({
  data,
  onChange,
}: {
  data: { operatorIds: string[]; testResults: Record<string, 'idle' | 'loading' | 'success' | 'error'>; testErrors: Record<string, string> }
  onChange: (data: { operatorIds: string[]; testResults: Record<string, 'idle' | 'loading' | 'success' | 'error'>; testErrors: Record<string, string> }) => void
}) {
  const { data: operators } = useOperatorList()

  const toggleOperator = (id: string) => {
    const ids = data.operatorIds.includes(id)
      ? data.operatorIds.filter((x) => x !== id)
      : [...data.operatorIds, id]
    onChange({ ...data, operatorIds: ids })
  }

  const handleTest = async (opId: string) => {
    onChange({ ...data, testResults: { ...data.testResults, [opId]: 'loading' }, testErrors: { ...data.testErrors, [opId]: '' } })
    try {
      const res = await api.post<ApiResponse<{ success: boolean; error?: string }>>(`/operators/${opId}/test`)
      const result = res.data.data
      if (result.success) {
        onChange({ ...data, testResults: { ...data.testResults, [opId]: 'success' } })
      } else {
        onChange({ ...data, testResults: { ...data.testResults, [opId]: 'error' }, testErrors: { ...data.testErrors, [opId]: result.error || 'Failed' } })
      }
    } catch {
      onChange({ ...data, testResults: { ...data.testResults, [opId]: 'error' }, testErrors: { ...data.testErrors, [opId]: 'Connection failed' } })
    }
  }

  return (
    <div className="space-y-3">
      <label className="block text-xs font-medium text-text-secondary">Select Operators * (one or more)</label>
      {!operators?.length ? (
        <p className="text-xs text-text-tertiary">No operators available. Ask a super_admin to create operators first.</p>
      ) : (
        <div className="space-y-2 max-h-64 overflow-y-auto pr-1">
          {operators.map((op) => {
            const selected = data.operatorIds.includes(op.id)
            const testState = data.testResults[op.id] || 'idle'
            return (
              <div
                key={op.id}
                className={cn(
                  'flex items-center justify-between rounded-[var(--radius-md)] border px-3 py-2.5 cursor-pointer transition-colors',
                  selected ? 'border-accent bg-accent/5' : 'border-border bg-bg-surface hover:border-text-tertiary',
                )}
                onClick={() => toggleOperator(op.id)}
              >
                <div className="flex items-center gap-3">
                  <div className={cn('h-4 w-4 rounded border flex items-center justify-center', selected ? 'border-accent bg-accent text-white' : 'border-border')}>
                    {selected && <Check className="h-3 w-3" />}
                  </div>
                  <div>
                    <span className="text-sm text-text-primary font-medium">{op.name}</span>
                    <span className="ml-2 text-xs text-text-tertiary font-mono">{op.code}</span>
                  </div>
                </div>
                {selected && (
                  <div className="flex items-center gap-2">
                    {testState === 'success' && <CheckCircle2 className="h-4 w-4 text-success" />}
                    {testState === 'error' && <AlertCircle className="h-4 w-4 text-danger" />}
                    {testState === 'loading' && <Spinner />}
                    {(testState === 'idle' || testState === 'error') && (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 px-2 text-xs gap-1"
                        onClick={(e) => { e.stopPropagation(); handleTest(op.id) }}
                      >
                        <Zap className="h-3 w-3" />
                        Test
                      </Button>
                    )}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
      {Object.entries(data.testErrors).filter(([, v]) => v).map(([id, err]) => (
        <div key={id} className="flex items-center gap-2 rounded-md border border-danger/30 bg-danger-dim px-3 py-1.5 text-xs text-danger">
          <AlertCircle className="h-3 w-3 shrink-0" />
          {operators?.find((o) => o.id === id)?.name}: {err}
        </div>
      ))}
    </div>
  )
}

function Step3APNConfig({
  data,
  onChange,
}: {
  data: { apnName: string; apnType: string; ipCidr: string }
  onChange: (data: { apnName: string; apnType: string; ipCidr: string }) => void
}) {
  return (
    <div className="space-y-4">
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">APN Name *</label>
        <Input
          value={data.apnName}
          onChange={(e) => onChange({ ...data, apnName: e.target.value })}
          placeholder="internet.acme.com"
          autoFocus
        />
      </div>
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">APN Type</label>
        <Select
          value={data.apnType}
          onChange={(e) => onChange({ ...data, apnType: e.target.value })}
          options={APN_TYPES}
        />
      </div>
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">IP Pool CIDR</label>
        <Input
          value={data.ipCidr}
          onChange={(e) => onChange({ ...data, ipCidr: e.target.value })}
          placeholder="10.0.0.0/24"
        />
        <p className="text-[10px] text-text-tertiary">Optional. You can configure IP pools later.</p>
      </div>
    </div>
  )
}

function Step4SIMImport({
  data,
  onChange,
}: {
  data: { importMode: 'csv' | 'manual'; csvFile: File | null; manualICCIDs: string }
  onChange: (data: { importMode: 'csv' | 'manual'; csvFile: File | null; manualICCIDs: string }) => void
}) {
  return (
    <div className="space-y-4">
      <div className="flex gap-2">
        <Button
          variant={data.importMode === 'csv' ? 'default' : 'outline'}
          size="sm"
          onClick={() => onChange({ ...data, importMode: 'csv' })}
          className="gap-1.5"
        >
          <Upload className="h-3.5 w-3.5" />
          CSV Upload
        </Button>
        <Button
          variant={data.importMode === 'manual' ? 'default' : 'outline'}
          size="sm"
          onClick={() => onChange({ ...data, importMode: 'manual' })}
        >
          Manual Entry
        </Button>
      </div>

      {data.importMode === 'csv' ? (
        <div className="space-y-1.5">
          <label className="block text-xs font-medium text-text-secondary">CSV File</label>
          <div className="flex items-center justify-center rounded-md border-2 border-dashed border-border bg-bg-elevated p-6 transition-colors hover:border-accent/50">
            <div className="text-center">
              <Upload className="mx-auto mb-2 h-8 w-8 text-text-tertiary" />
              <label className="cursor-pointer text-sm text-accent hover:underline">
                Choose file
                <FileInput
                  accept=".csv"
                  className="hidden"
                  onChange={(e) => onChange({ ...data, csvFile: e.target.files?.[0] || null })}
                />
              </label>
              {data.csvFile && (
                <p className="mt-2 text-xs text-text-secondary">{data.csvFile.name}</p>
              )}
              <p className="mt-1 text-[10px] text-text-tertiary">CSV with columns: iccid, imsi, msisdn (optional)</p>
            </div>
          </div>
        </div>
      ) : (
        <div className="space-y-1.5">
          <label className="block text-xs font-medium text-text-secondary">ICCIDs (one per line)</label>
          <Textarea
            value={data.manualICCIDs}
            onChange={(e) => onChange({ ...data, manualICCIDs: e.target.value })}
            placeholder="8901260882168430001&#10;8901260882168430002&#10;8901260882168430003"
            rows={5}
            className="font-mono"
          />
        </div>
      )}
    </div>
  )
}

function Step5PolicySetup({
  data,
  onChange,
}: {
  data: { policyName: string; template: string; dslSource: string }
  onChange: (data: { policyName: string; template: string; dslSource: string }) => void
}) {
  return (
    <div className="space-y-4">
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">Policy Name</label>
        <Input
          value={data.policyName}
          onChange={(e) => onChange({ ...data, policyName: e.target.value })}
          placeholder="Default Access Policy"
          autoFocus
        />
      </div>
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">Template</label>
        <Select
          value={data.template}
          onChange={(e) => {
            const tpl = POLICY_TEMPLATES.find((t) => t.value === e.target.value)
            onChange({ ...data, template: e.target.value, dslSource: tpl?.dsl || '' })
          }}
          options={POLICY_TEMPLATES.map((t) => ({ value: t.value, label: t.label }))}
        />
      </div>
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">Policy DSL</label>
        <Textarea
          value={data.dslSource}
          onChange={(e) => onChange({ ...data, dslSource: e.target.value })}
          placeholder="WHEN subscriber.state = &quot;active&quot;&#10;THEN ALLOW&#10;  rate_limit = 10mbps"
          rows={6}
          className="font-mono"
        />
      </div>
    </div>
  )
}

export function OnboardingWizard() {
  const navigate = useNavigate()
  const setOnboardingCompleted = useAuthStore((s) => s.setOnboardingCompleted)

  const [sessionID, setSessionID] = useState<string | null>(() => getStoredSessionID())
  const start = useStartOnboarding()
  const session = useOnboardingSession(sessionID)
  const submitStep = useSubmitOnboardingStep(sessionID)
  const completeOnboarding = useCompleteOnboarding(sessionID)

  const [currentStep, setCurrentStep] = useState(1)
  const [completedSteps, setCompletedSteps] = useState<Set<number>>(new Set())
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Bootstrap: create or resume an onboarding session on mount.
  useEffect(() => {
    if (sessionID) return
    start
      .mutateAsync()
      .then((res) => {
        setSessionID(res.session_id)
        setCurrentStep(res.current_step || 1)
      })
      .catch((err) => {
        const msg = err instanceof Error ? err.message : 'Failed to start onboarding session'
        setError(msg)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Resume: when the session loads, jump to its current step.
  useEffect(() => {
    if (!session.data) return
    if (session.data.current_step) {
      setCurrentStep(session.data.current_step)
    }
    if (session.data.current_step && session.data.current_step > 1) {
      setCompletedSteps(new Set(Array.from({ length: session.data.current_step - 1 }, (_, i) => i + 1)))
    }
  }, [session.data])

  const [step1, setStep1] = useState({ companyName: '', timezone: 'UTC', retentionDays: '90', contactEmail: '', locale: 'en' })
  const [step2, setStep2] = useState<{ operatorIds: string[]; testResults: Record<string, 'idle' | 'loading' | 'success' | 'error'>; testErrors: Record<string, string> }>({ operatorIds: [], testResults: {}, testErrors: {} })
  const [step3, setStep3] = useState({ apnName: '', apnType: 'internet', ipCidr: '' })
  const [step4, setStep4] = useState<{ importMode: 'csv' | 'manual'; csvFile: File | null; manualICCIDs: string }>({ importMode: 'csv', csvFile: null, manualICCIDs: '' })
  const [step5, setStep5] = useState({ policyName: '', template: 'basic-internet', dslSource: POLICY_TEMPLATES[0].dsl })

  function canProceed(): boolean {
    switch (currentStep) {
      case 1:
        return step1.companyName.trim().length > 0 && step1.contactEmail.trim().length > 0 && step1.contactEmail.includes('@')
      case 2:
        return step2.operatorIds.length > 0 && step2.operatorIds.every((id) => step2.testResults[id] === 'success')
      case 3:
        return step3.apnName.trim().length > 0
      case 4:
        return true
      case 5:
        return true
      default:
        return false
    }
  }

  function isSkippable(): boolean {
    return !STEPS[currentStep - 1].mandatory
  }

  function payloadForStep(step: number): Record<string, unknown> {
    switch (step) {
      case 1:
        return {
          company_name: step1.companyName,
          contact_email: step1.contactEmail,
          locale: step1.locale,
          timezone: step1.timezone,
          data_retention_days: parseInt(step1.retentionDays, 10),
        }
      case 2:
        return {
          operator_grants: step2.operatorIds.map((id) => ({ operator_id: id, rat_types: [] })),
        }
      case 3:
        return {
          apn_name: step3.apnName,
          apn_type: step3.apnType,
          ip_cidr: step3.ipCidr,
        }
      case 4:
        return {
          import_mode: step4.importMode,
          iccids: step4.importMode === 'manual'
            ? step4.manualICCIDs.trim().split('\n').filter(Boolean)
            : [],
          csv_s3_key: '',
        }
      case 5:
        return {
          policy_name: step5.policyName,
          dsl_source: step5.dslSource,
        }
      default:
        return {}
    }
  }

  async function handleNext() {
    if (!sessionID) {
      setError('Onboarding session is not ready')
      return
    }
    setError(null)
    setSubmitting(true)

    try {
      await submitStep.mutateAsync({ step: currentStep, payload: payloadForStep(currentStep) })
      setCompletedSteps((prev) => new Set([...prev, currentStep]))

      if (currentStep < 5) {
        setCurrentStep(currentStep + 1)
      } else {
        await completeSetup()
      }
    } catch (err: unknown) {
      const axiosError = err as { response?: { data?: { error?: { message?: string } } } }
      setError(axiosError.response?.data?.error?.message || 'An error occurred. Please try again.')
    } finally {
      setSubmitting(false)
    }
  }

  function handleSkip() {
    setCompletedSteps((prev) => new Set([...prev, currentStep]))
    if (currentStep < 5) {
      setCurrentStep(currentStep + 1)
    } else {
      completeSetup()
    }
  }

  function handleBack() {
    if (currentStep > 1) {
      setCurrentStep(currentStep - 1)
      setError(null)
    }
  }

  async function completeSetup() {
    try {
      await completeOnboarding.mutateAsync()
    } catch {
      // even on completion error we navigate — server-side state may have advanced
    }
    setStoredSessionID(null)
    setOnboardingCompleted(true)
    navigate('/', { replace: true })
  }

  const stepConfig = STEPS[currentStep - 1]

  return (
    <div className="mx-auto max-w-2xl">
      <div className="mb-6 text-center">
        <h1 className="text-xl font-semibold text-text-primary">Welcome to Argus</h1>
        <p className="mt-1 text-sm text-text-secondary">Let's set up your environment in a few steps</p>
      </div>

      <StepIndicator steps={STEPS} currentStep={currentStep} completedSteps={completedSteps} />

      <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
        <div className="mb-5">
          <h2 className="text-[15px] font-semibold text-text-primary">{stepConfig.title}</h2>
          <p className="text-sm text-text-secondary">{stepConfig.description}</p>
        </div>

        {error && (
          <div className="mb-4 flex items-start gap-2 rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-sm text-danger">
            <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
            <div className="flex-1">
              <p>{error}</p>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleNext}
                disabled={submitting}
                className="mt-1.5 h-auto px-0 text-xs underline underline-offset-2 hover:no-underline text-danger"
              >
                Retry this step
              </Button>
            </div>
          </div>
        )}

        {currentStep === 1 && <Step1TenantProfile data={step1} onChange={setStep1} />}
        {currentStep === 2 && <Step2OperatorConnection data={step2} onChange={setStep2} />}
        {currentStep === 3 && <Step3APNConfig data={step3} onChange={setStep3} />}
        {currentStep === 4 && <Step4SIMImport data={step4} onChange={setStep4} />}
        {currentStep === 5 && <Step5PolicySetup data={step5} onChange={setStep5} />}

        <div className="mt-6 flex items-center justify-between border-t border-border pt-4">
          <Button variant="ghost" onClick={handleBack} disabled={currentStep === 1 || submitting} className="gap-1.5">
            <ChevronLeft className="h-4 w-4" />
            Back
          </Button>

          <div className="flex items-center gap-2">
            {isSkippable() && (
              <Button variant="ghost" onClick={handleSkip} disabled={submitting} className="gap-1.5">
                <SkipForward className="h-4 w-4" />
                Skip
              </Button>
            )}
            <Button onClick={handleNext} disabled={!canProceed() || submitting} className="gap-1.5">
              {submitting ? (
                <>
                  <Spinner />
                  Saving...
                </>
              ) : currentStep === 5 ? (
                <>
                  <Check className="h-4 w-4" />
                  Complete Setup
                </>
              ) : (
                <>
                  Next
                  <ChevronRight className="h-4 w-4" />
                </>
              )}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
