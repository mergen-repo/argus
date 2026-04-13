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
import { Select } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { FileInput } from '@/components/ui/file-input'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import { useOperatorList, useTestConnection } from '@/hooks/use-operators'

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
    <div className="flex items-center justify-center gap-2 py-6">
      {steps.map((step, idx) => {
        const isCompleted = completedSteps.has(step.id)
        const isCurrent = step.id === currentStep
        const Icon = step.icon
        return (
          <div key={step.id} className="flex items-center">
            <div className="flex flex-col items-center">
              <div
                className={cn(
                  'flex h-10 w-10 items-center justify-center rounded-full border-2 transition-all duration-200',
                  isCompleted && 'border-success bg-success text-white',
                  isCurrent && !isCompleted && 'border-accent bg-accent-dim text-accent',
                  !isCurrent && !isCompleted && 'border-border bg-bg-surface text-text-tertiary',
                )}
              >
                {isCompleted ? <Check className="h-5 w-5" /> : <Icon className="h-5 w-5" />}
              </div>
              <span className={cn('mt-1.5 text-[10px] font-medium', isCurrent ? 'text-text-primary' : 'text-text-tertiary')}>
                {step.title}
              </span>
            </div>
            {idx < steps.length - 1 && (
              <div className={cn('mx-2 h-0.5 w-8 rounded-full transition-colors', isCompleted ? 'bg-success' : 'bg-border')} />
            )}
          </div>
        )
      })}
    </div>
  )
}

function Step1TenantProfile({
  data,
  onChange,
}: {
  data: { companyName: string; timezone: string; retentionDays: string }
  onChange: (data: { companyName: string; timezone: string; retentionDays: string }) => void
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
        <label className="block text-xs font-medium text-text-secondary">Timezone</label>
        <Select
          value={data.timezone}
          onChange={(e) => onChange({ ...data, timezone: e.target.value })}
          options={TIMEZONES}
        />
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
  data: { operatorId: string; testResult: 'idle' | 'loading' | 'success' | 'error'; testError?: string }
  onChange: (data: { operatorId: string; testResult: 'idle' | 'loading' | 'success' | 'error'; testError?: string }) => void
}) {
  const { data: operators } = useOperatorList()
  const testConnection = useTestConnection(data.operatorId)

  const operatorOptions = (operators || []).map((op) => ({
    value: op.id,
    label: `${op.name} (${op.code})`,
  }))

  const handleTest = async () => {
    if (!data.operatorId) return
    onChange({ ...data, testResult: 'loading', testError: undefined })
    try {
      const result = await testConnection.mutateAsync()
      if (result.success) {
        onChange({ ...data, testResult: 'success', testError: undefined })
      } else {
        onChange({ ...data, testResult: 'error', testError: result.error || 'Connection test failed' })
      }
    } catch {
      onChange({ ...data, testResult: 'error', testError: 'Failed to test connection' })
    }
  }

  return (
    <div className="space-y-4">
      <div className="space-y-1.5">
        <label className="block text-xs font-medium text-text-secondary">Select Operator *</label>
        <Select
          value={data.operatorId}
          onChange={(e) => onChange({ ...data, operatorId: e.target.value, testResult: 'idle', testError: undefined })}
          options={operatorOptions}
          placeholder="Choose an operator"
        />
      </div>

      {data.operatorId && (
        <div className="space-y-3">
          <Button
            variant="outline"
            onClick={handleTest}
            disabled={data.testResult === 'loading'}
            className="w-full gap-2"
          >
            {data.testResult === 'loading' ? (
              <>
                <Spinner />
                Testing connection...
              </>
            ) : (
              <>
                <Zap className="h-4 w-4" />
                Test Connection
              </>
            )}
          </Button>

          {data.testResult === 'success' && (
            <div className="flex items-center gap-2 rounded-md border border-success/30 bg-success-dim px-3 py-2 text-sm text-success">
              <CheckCircle2 className="h-4 w-4" />
              Connection successful
            </div>
          )}

          {data.testResult === 'error' && (
            <div className="space-y-2">
              <div className="flex items-center gap-2 rounded-md border border-danger/30 bg-danger-dim px-3 py-2 text-sm text-danger">
                <AlertCircle className="h-4 w-4" />
                {data.testError}
              </div>
              <Button variant="outline" size="sm" onClick={handleTest} className="gap-1.5">
                Retry
              </Button>
            </div>
          )}
        </div>
      )}
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
          <textarea
            value={data.manualICCIDs}
            onChange={(e) => onChange({ ...data, manualICCIDs: e.target.value })}
            placeholder="8901260882168430001&#10;8901260882168430002&#10;8901260882168430003"
            rows={5}
            className="flex w-full rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-tertiary focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent focus-visible:border-accent font-mono"
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
        <textarea
          value={data.dslSource}
          onChange={(e) => onChange({ ...data, dslSource: e.target.value })}
          placeholder="WHEN subscriber.state = &quot;active&quot;&#10;THEN ALLOW&#10;  rate_limit = 10mbps"
          rows={6}
          className="flex w-full rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-tertiary focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent focus-visible:border-accent font-mono"
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

  const [step1, setStep1] = useState({ companyName: '', timezone: 'UTC', retentionDays: '90' })
  const [step2, setStep2] = useState<{ operatorId: string; testResult: 'idle' | 'loading' | 'success' | 'error'; testError?: string }>({ operatorId: '', testResult: 'idle' })
  const [step3, setStep3] = useState({ apnName: '', apnType: 'internet', ipCidr: '' })
  const [step4, setStep4] = useState<{ importMode: 'csv' | 'manual'; csvFile: File | null; manualICCIDs: string }>({ importMode: 'csv', csvFile: null, manualICCIDs: '' })
  const [step5, setStep5] = useState({ policyName: '', template: 'basic-internet', dslSource: POLICY_TEMPLATES[0].dsl })

  function canProceed(): boolean {
    switch (currentStep) {
      case 1:
        return step1.companyName.trim().length > 0
      case 2:
        return step2.operatorId.length > 0 && step2.testResult === 'success'
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
          contact_email: '',
          locale: step1.timezone,
        }
      case 2:
        return {
          operator_grants: step2.operatorId
            ? [{ operator_id: step2.operatorId, rat_types: [] }]
            : [],
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
          <div className="mb-4 flex items-center gap-2 rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-sm text-danger">
            <AlertCircle className="h-4 w-4 shrink-0" />
            {error}
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
