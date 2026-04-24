import { useState, type FormEvent } from 'react'
import { useNavigate, Link } from 'react-router-dom'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Spinner } from '@/components/ui/spinner'

export default function LoginPage() {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)
  const setPartial2FA = useAuthStore((s) => s.setPartial2FA)
  const setPartialSession = useAuthStore((s) => s.setPartialSession)

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [rememberMe, setRememberMe] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [lockout, setLockout] = useState<{ message: string; retryAfter: number } | null>(null)
  const [lockoutTimer, setLockoutTimer] = useState(0)

  const [fieldErrors, setFieldErrors] = useState<{ email?: string; password?: string }>({})

  function validate(): boolean {
    const errors: { email?: string; password?: string } = {}
    if (!email.trim()) {
      errors.email = 'Email is required'
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      errors.email = 'Invalid email format'
    }
    if (!password) {
      errors.password = 'Password is required'
    }
    setFieldErrors(errors)
    return Object.keys(errors).length === 0
  }

  function startLockoutCountdown(seconds: number) {
    setLockoutTimer(seconds)
    const interval = setInterval(() => {
      setLockoutTimer((prev) => {
        if (prev <= 1) {
          clearInterval(interval)
          setLockout(null)
          return 0
        }
        return prev - 1
      })
    }, 1000)
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setLockout(null)

    if (!validate()) return

    setLoading(true)
    try {
      const res = await authApi.login(email, password, rememberMe)
      const data = res.data.data

      if (data.partial === true && data.reason === 'password_change_required') {
        setPartialSession(data.token, data.reason)
        navigate('/auth/change-password')
      } else if (data.requires_2fa) {
        setPartial2FA(data.token, data.user)
        navigate('/login/2fa')
      } else {
        setAuth(data.user, data.token, [], data.session_id)
        if (data.user.onboarding_completed === false) {
          navigate('/setup')
        } else {
          navigate('/')
        }
      }
    } catch (err: unknown) {
      const axiosError = err as { response?: { status?: number; data?: { error?: { message?: string; details?: Array<Record<string, unknown>> } } } }
      const status = axiosError.response?.status
      const errorData = axiosError.response?.data?.error

      if (status === 423 || (status === 403 && errorData?.message?.includes('locked'))) {
        const details = errorData?.details?.[0]
        const retryAfter = (details?.retry_after_seconds as number) || 300
        setLockout({
          message: errorData?.message || 'Account locked due to too many failed attempts.',
          retryAfter,
        })
        startLockoutCountdown(retryAfter)
      } else if (status === 401) {
        setError(errorData?.message || 'Invalid email or password')
      } else {
        setError(errorData?.message || 'An unexpected error occurred')
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <div className="mb-2">
        <h2 className="text-[15px] font-semibold text-text-primary">Sign in</h2>
        <p className="text-sm text-text-secondary">Enter your credentials to continue</p>
      </div>

      {error && (
        <div className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}

      {lockout && (
        <div className="rounded-[var(--radius-sm)] border border-warning/30 bg-warning-dim px-3 py-2.5 text-sm">
          <p className="font-medium text-warning">{lockout.message}</p>
          {lockoutTimer > 0 && (
            <p className="mt-1 font-mono text-xs text-text-secondary">
              Retry in {Math.floor(lockoutTimer / 60)}:{String(lockoutTimer % 60).padStart(2, '0')}
            </p>
          )}
        </div>
      )}

      <div className="space-y-1.5">
        <label htmlFor="email" className="block text-xs font-medium text-text-secondary">
          Email
        </label>
        <Input
          id="email"
          type="email"
          placeholder="you@company.com"
          value={email}
          onChange={(e) => {
            setEmail(e.target.value)
            setFieldErrors((prev) => ({ ...prev, email: undefined }))
          }}
          className={cn(fieldErrors.email && 'border-danger focus-visible:ring-danger')}
          autoComplete="email"
          autoFocus
          disabled={loading || !!lockout}
        />
        {fieldErrors.email && (
          <p className="text-xs text-danger">{fieldErrors.email}</p>
        )}
      </div>

      <div className="space-y-1.5">
        <label htmlFor="password" className="block text-xs font-medium text-text-secondary">
          Password
        </label>
        <Input
          id="password"
          type="password"
          placeholder="Enter your password"
          value={password}
          onChange={(e) => {
            setPassword(e.target.value)
            setFieldErrors((prev) => ({ ...prev, password: undefined }))
          }}
          className={cn(fieldErrors.password && 'border-danger focus-visible:ring-danger')}
          autoComplete="current-password"
          disabled={loading || !!lockout}
        />
        {fieldErrors.password && (
          <p className="text-xs text-danger">{fieldErrors.password}</p>
        )}
      </div>

      <div className="flex items-center gap-2">
        <Input
          id="remember"
          type="checkbox"
          checked={rememberMe}
          onChange={(e) => setRememberMe(e.target.checked)}
          className="h-3.5 w-3.5 rounded border-border bg-bg-elevated text-accent accent-accent focus:ring-accent focus:ring-offset-0 w-3.5 flex-none"
          disabled={loading || !!lockout}
        />
        <label htmlFor="remember" className="text-xs text-text-secondary cursor-pointer select-none">
          Remember me
        </label>
      </div>

      <Button
        type="submit"
        className="w-full"
        disabled={loading || !!lockout}
      >
        {loading ? (
          <span className="flex items-center gap-2">
            <Spinner />
            Signing in...
          </span>
        ) : (
          'Sign in'
        )}
      </Button>

      <div className="mt-3 text-center">
        <Link to="/auth/forgot" className="text-xs text-text-secondary hover:text-text-primary">
          Parolamı unuttum?
        </Link>
      </div>
    </form>
  )
}
