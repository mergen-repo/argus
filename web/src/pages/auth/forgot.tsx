import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { authApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Spinner } from '@/components/ui/spinner'
import { MailCheck } from 'lucide-react'

const GENERIC_SUCCESS = 'Eğer bu adres kayıtlı ise, sıfırlama bağlantısı e-posta ile gönderildi.'
const RATE_LIMITED_MSG = 'Çok fazla istek. Lütfen bir saat sonra tekrar deneyin.'

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState('')
  const [loading, setLoading] = useState(false)
  const [submitted, setSubmitted] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<{ email?: string }>({})

  function validate(): boolean {
    const errors: { email?: string } = {}
    if (!email.trim()) {
      errors.email = 'E-posta adresi gereklidir'
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      errors.email = 'Geçersiz e-posta formatı'
    }
    setFieldErrors(errors)
    return Object.keys(errors).length === 0
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)

    if (!validate()) return

    setLoading(true)
    try {
      await authApi.requestPasswordReset(email)
      setSubmitted(true)
    } catch (err: unknown) {
      const axiosError = err as { response?: { status?: number } }
      const status = axiosError.response?.status
      if (status === 429) {
        setError(RATE_LIMITED_MSG)
      } else {
        setSubmitted(true)
      }
    } finally {
      setLoading(false)
    }
  }

  if (submitted) {
    return (
      <div className="space-y-4">
        <div className="mb-2">
          <h2 className="text-[15px] font-semibold text-text-primary">Şifre sıfırlama</h2>
        </div>
        <div className="flex flex-col items-center gap-3 rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-4 py-6 text-center">
          <MailCheck className="h-8 w-8 text-accent" strokeWidth={1.5} />
          <p className="text-sm text-text-secondary">{GENERIC_SUCCESS}</p>
        </div>
        <div className="text-center">
          <Link to="/login" className="text-xs text-text-secondary hover:text-text-primary">
            Girişe dön
          </Link>
        </div>
      </div>
    )
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <div className="mb-2">
        <h2 className="text-[15px] font-semibold text-text-primary">Şifre sıfırlama</h2>
        <p className="text-sm text-text-secondary">E-posta adresinizi girin, size sıfırlama bağlantısı göndereceğiz.</p>
      </div>

      {error && (
        <div className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}

      <div className="space-y-1.5">
        <label htmlFor="email" className="block text-xs font-medium text-text-secondary">
          E-posta
        </label>
        <Input
          id="email"
          type="email"
          placeholder="siz@sirket.com"
          value={email}
          onChange={(e) => {
            setEmail(e.target.value)
            setFieldErrors((prev) => ({ ...prev, email: undefined }))
          }}
          className={cn(fieldErrors.email && 'border-danger focus-visible:ring-danger')}
          autoComplete="email"
          autoFocus
          aria-invalid={!!fieldErrors.email}
          disabled={loading}
        />
        {fieldErrors.email && (
          <p className="text-xs text-danger">{fieldErrors.email}</p>
        )}
      </div>

      <Button type="submit" className="w-full" disabled={loading}>
        {loading ? (
          <span className="flex items-center gap-2">
            <Spinner />
            Gönderiliyor...
          </span>
        ) : (
          'Sıfırlama bağlantısı gönder'
        )}
      </Button>

      <div className="mt-3 text-center">
        <Link to="/login" className="text-xs text-text-secondary hover:text-text-primary">
          Girişe dön
        </Link>
      </div>
    </form>
  )
}
