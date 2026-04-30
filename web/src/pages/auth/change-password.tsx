import { useState, useMemo, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Spinner } from '@/components/ui/spinner'
import { toast } from 'sonner'

interface PolicyRule {
  id: string
  label: string
  check: (pw: string) => boolean
}

const PASSWORD_RULES: PolicyRule[] = [
  {
    id: 'length',
    label: 'At least 12 characters',
    check: (pw) => pw.length >= 12,
  },
  {
    id: 'complexity',
    label: 'Upper + lower + digit + symbol',
    check: (pw) =>
      /[a-z]/.test(pw) && /[A-Z]/.test(pw) && /\d/.test(pw) && /[^a-zA-Z0-9]/.test(pw),
  },
  {
    id: 'repeating',
    label: 'Not more than 3 repeating characters',
    check: (pw) => !/(.)\1{3,}/.test(pw),
  },
  {
    id: 'history',
    label: 'Not one of last 5 passwords',
    check: () => true,
  },
]

type AxiosLikeError = {
  response?: {
    status?: number
    data?: {
      error?: {
        code?: string
        message?: string
      }
    }
  }
}

export default function ChangePasswordPage() {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)
  const clearPartial = useAuthStore((s) => s.clearPartial)
  const user = useAuthStore((s) => s.user)
  const partialToken = useAuthStore((s) => s.partialToken)

  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [serverError, setServerError] = useState<string | null>(null)
  const [failingRuleId, setFailingRuleId] = useState<string | null>(null)
  const [showCurrent, setShowCurrent] = useState(false)
  const [showNew, setShowNew] = useState(false)
  const [showConfirm, setShowConfirm] = useState(false)

  const ruleStatus = useMemo(
    () =>
      PASSWORD_RULES.map((rule) => ({
        ...rule,
        passed: rule.id === 'history' ? true : newPassword.length > 0 && rule.check(newPassword),
        active: newPassword.length > 0,
      })),
    [newPassword],
  )

  const allClientRulesPassed =
    newPassword.length > 0 &&
    PASSWORD_RULES.every((rule) => rule.id === 'history' || rule.check(newPassword))

  const passwordsMatch = newPassword.length > 0 && newPassword === confirmPassword
  const canSubmit = currentPassword.length > 0 && allClientRulesPassed && passwordsMatch

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!canSubmit) return

    setServerError(null)
    setFailingRuleId(null)
    setLoading(true)

    try {
      const res = await authApi.changePassword(currentPassword, newPassword)
      const data = res.data.data

      if (user && 'token' in data) {
        const token = (data as { token: string; message: string }).token
        setAuth(user, token)
      } else {
        clearPartial()
      }
      navigate('/', { replace: true })
    } catch (err: unknown) {
      const axiosError = err as AxiosLikeError
      const status = axiosError.response?.status
      const errorData = axiosError.response?.data?.error
      const code = errorData?.code ?? ''
      const message = errorData?.message ?? 'An unexpected error occurred'

      if (status === 401) {
        setServerError('Current password is incorrect')
      } else if (code === 'PASSWORD_REUSED') {
        toast.error('New password matches a recent password — choose a different one')
      } else if (code.startsWith('PASSWORD_')) {
        setFailingRuleId(code)
        setServerError(message)
      } else {
        setServerError(message)
      }
    } finally {
      setLoading(false)
    }
  }

  const eyeIcon = (visible: boolean) => (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="h-4 w-4"
      aria-hidden="true"
    >
      {visible ? (
        <>
          <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
          <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
          <line x1="1" y1="1" x2="23" y2="23" />
        </>
      ) : (
        <>
          <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
          <circle cx="12" cy="12" r="3" />
        </>
      )}
    </svg>
  )

  const strengthLevel = ruleStatus.filter((r) => r.passed && r.active).length
  const strengthColors = ['bg-border', 'bg-danger', 'bg-warning', 'bg-warning', 'bg-success']
  const strengthLabels = ['', 'Weak', 'Fair', 'Good', 'Strong']

  return (
    <form onSubmit={handleSubmit} className="space-y-5" noValidate>
      <div className="mb-1">
        <h2 className="text-[15px] font-semibold text-text-primary">Change Your Password</h2>
        <p className="text-sm text-text-secondary mt-0.5">
          Your account requires a password change before you can continue.
        </p>
      </div>

      {serverError && (
        <div
          role="alert"
          className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2.5 text-sm text-danger"
        >
          {serverError}
        </div>
      )}

      <div className="space-y-1.5">
        <label htmlFor="current-password" className="block text-xs font-medium text-text-secondary">
          Current password
        </label>
        <div className="relative">
          <Input
            id="current-password"
            type={showCurrent ? 'text' : 'password'}
            placeholder="Enter current password"
            value={currentPassword}
            onChange={(e) => {
              setCurrentPassword(e.target.value)
              setServerError(null)
            }}
            autoComplete="current-password"
            autoFocus
            disabled={loading}
            className="pr-9"
            aria-label="Current password"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => setShowCurrent((v) => !v)}
            className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7 text-text-tertiary hover:text-text-secondary"
            aria-label={showCurrent ? 'Hide password' : 'Show password'}
            tabIndex={-1}
          >
            {eyeIcon(showCurrent)}
          </Button>
        </div>
      </div>

      <div className="space-y-1.5">
        <label htmlFor="new-password" className="block text-xs font-medium text-text-secondary">
          New password
        </label>
        <div className="relative">
          <Input
            id="new-password"
            type={showNew ? 'text' : 'password'}
            placeholder="Create a strong password"
            value={newPassword}
            onChange={(e) => {
              setNewPassword(e.target.value)
              setServerError(null)
              setFailingRuleId(null)
            }}
            autoComplete="new-password"
            disabled={loading}
            className="pr-9"
            aria-label="New password"
            aria-describedby="password-rules"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => setShowNew((v) => !v)}
            className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7 text-text-tertiary hover:text-text-secondary"
            aria-label={showNew ? 'Hide password' : 'Show password'}
            tabIndex={-1}
          >
            {eyeIcon(showNew)}
          </Button>
        </div>

        {newPassword.length > 0 && (
          <div className="mt-1.5 flex items-center gap-1.5">
            {[1, 2, 3, 4].map((i) => (
              <div
                key={i}
                className={cn(
                  'h-1 flex-1 rounded-full transition-all duration-300',
                  i <= strengthLevel ? strengthColors[strengthLevel] : 'bg-border',
                )}
              />
            ))}
            <span className="ml-1 text-[11px] font-medium text-text-tertiary w-10 shrink-0">
              {strengthLabels[strengthLevel]}
            </span>
          </div>
        )}

        <ul
          id="password-rules"
          className="mt-2 space-y-1"
          role="list"
          aria-label="Password requirements"
        >
          {ruleStatus.map((rule) => {
            const isHighlighted = failingRuleId && rule.id.toLowerCase().includes(failingRuleId.toLowerCase())
            return (
              <li
                key={rule.id}
                className={cn(
                  'flex items-center gap-2 text-xs transition-colors duration-200',
                  rule.active
                    ? rule.passed
                      ? 'text-success'
                      : isHighlighted
                        ? 'text-danger'
                        : 'text-danger'
                    : 'text-text-tertiary',
                )}
              >
                <span
                  className={cn(
                    'flex h-4 w-4 shrink-0 items-center justify-center rounded-full border text-[10px] transition-all duration-200',
                    rule.active
                      ? rule.passed
                        ? 'border-success bg-success-dim text-success'
                        : 'border-danger/40 bg-danger-dim text-danger'
                      : 'border-border-subtle text-text-tertiary',
                  )}
                  aria-hidden="true"
                >
                  {rule.active ? (rule.passed ? '✓' : '✗') : '·'}
                </span>
                <span>{rule.label}</span>
              </li>
            )
          })}
        </ul>
      </div>

      <div className="space-y-1.5">
        <label htmlFor="confirm-password" className="block text-xs font-medium text-text-secondary">
          Confirm new password
        </label>
        <div className="relative">
          <Input
            id="confirm-password"
            type={showConfirm ? 'text' : 'password'}
            placeholder="Repeat new password"
            value={confirmPassword}
            onChange={(e) => setConfirmPassword(e.target.value)}
            autoComplete="new-password"
            disabled={loading}
            className={cn(
              'pr-9',
              confirmPassword.length > 0 && !passwordsMatch && 'border-danger focus-visible:ring-danger',
              confirmPassword.length > 0 && passwordsMatch && 'border-success/50 focus-visible:ring-success/30',
            )}
            aria-label="Confirm new password"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => setShowConfirm((v) => !v)}
            className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7 text-text-tertiary hover:text-text-secondary"
            aria-label={showConfirm ? 'Hide password' : 'Show password'}
            tabIndex={-1}
          >
            {eyeIcon(showConfirm)}
          </Button>
        </div>
        {confirmPassword.length > 0 && !passwordsMatch && (
          <p className="text-xs text-danger" role="alert">
            Passwords do not match
          </p>
        )}
        {confirmPassword.length > 0 && passwordsMatch && (
          <p className="text-xs text-success">Passwords match</p>
        )}
      </div>

      {!partialToken && (
        <div className="rounded-[var(--radius-sm)] border border-warning/30 bg-warning-dim px-3 py-2 text-xs text-warning">
          No active session — please log in first.
        </div>
      )}

      <Button
        type="submit"
        className="w-full"
        disabled={!canSubmit || loading}
        aria-disabled={!canSubmit || loading}
      >
        {loading ? (
          <span className="flex items-center gap-2">
            <Spinner />
            Changing password...
          </span>
        ) : (
          'Change Password'
        )}
      </Button>
    </form>
  )
}
