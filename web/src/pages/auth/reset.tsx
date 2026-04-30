import { useState, type FormEvent, useEffect } from 'react'
import { useNavigate, useSearchParams, Link } from 'react-router-dom'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { authApi } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Spinner } from '@/components/ui/spinner'
import { Eye, EyeOff } from 'lucide-react'
import { toast } from 'sonner'

const INVALID_TOKEN_MSG = 'Bu sıfırlama bağlantısı geçersiz veya süresi dolmuş. Yeni bir istek oluşturun.'
const POLICY_HINT = 'En az 8 karakter, rakam ve harf içermelidir.'

export default function ResetPasswordPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const token = searchParams.get('token')

  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [tokenInvalid, setTokenInvalid] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [showConfirm, setShowConfirm] = useState(false)
  const [fieldErrors, setFieldErrors] = useState<{ password?: string; confirm?: string }>({})

  useEffect(() => {
    if (!token) {
      setTokenInvalid(true)
    }
  }, [token])

  function validate(): boolean {
    const errors: { password?: string; confirm?: string } = {}
    if (!password) {
      errors.password = 'Yeni şifre gereklidir'
    } else if (password.length < 8) {
      errors.password = 'Şifre en az 8 karakter olmalıdır'
    } else if (!/[0-9]/.test(password) || !/[a-zA-Z]/.test(password)) {
      errors.password = 'Şifre rakam ve harf içermelidir'
    }
    if (!confirm) {
      errors.confirm = 'Şifreyi tekrar girin'
    } else if (password && confirm !== password) {
      errors.confirm = 'Şifreler eşleşmiyor'
    }
    setFieldErrors(errors)
    return Object.keys(errors).length === 0
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)

    if (!validate()) return
    if (!token) return

    setLoading(true)
    try {
      await authApi.confirmPasswordReset(token, password)
      toast.success('Şifreniz başarıyla sıfırlandı.')
      navigate('/login')
    } catch (err: unknown) {
      const axiosError = err as { response?: { status?: number; data?: { error?: { code?: string; message?: string } } } }
      const status = axiosError.response?.status
      const errorCode = axiosError.response?.data?.error?.code
      const errorMessage = axiosError.response?.data?.error?.message

      if (status === 400 && errorCode === 'PASSWORD_RESET_INVALID_TOKEN') {
        setTokenInvalid(true)
      } else if (status === 422) {
        if (errorCode === 'PASSWORD_TOO_SHORT') {
          setFieldErrors((prev) => ({ ...prev, password: 'Şifre en az 8 karakter olmalıdır' }))
        } else if (errorCode === 'PASSWORD_NO_DIGIT') {
          setFieldErrors((prev) => ({ ...prev, password: 'Şifre en az bir rakam içermelidir' }))
        } else if (errorCode === 'PASSWORD_NO_LETTER') {
          setFieldErrors((prev) => ({ ...prev, password: 'Şifre en az bir harf içermelidir' }))
        } else {
          setError(errorMessage || 'Şifre politikasına uygun değil.')
        }
      } else {
        setError(errorMessage || 'Bir hata oluştu. Lütfen tekrar deneyin.')
      }
    } finally {
      setLoading(false)
    }
  }

  if (tokenInvalid) {
    return (
      <div className="space-y-4">
        <div className="mb-2">
          <h2 className="text-[15px] font-semibold text-text-primary">Şifre sıfırlama</h2>
        </div>
        <div className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-3 text-sm text-danger">
          {INVALID_TOKEN_MSG}
        </div>
        <div className="text-center">
          <Link to="/auth/forgot" className="text-xs text-text-secondary hover:text-text-primary">
            Yeni istek oluştur
          </Link>
        </div>
      </div>
    )
  }

  if (!token) return null

  return (
    <form onSubmit={handleSubmit} className="space-y-4" noValidate>
      <div className="mb-2">
        <h2 className="text-[15px] font-semibold text-text-primary">Yeni şifre belirle</h2>
        <p className="text-sm text-text-secondary">Hesabınız için yeni bir şifre oluşturun.</p>
      </div>

      {error && (
        <div className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}

      <div className="space-y-1.5">
        <label htmlFor="password" className="block text-xs font-medium text-text-secondary">
          Yeni şifre
        </label>
        <div className="relative">
          <Input
            id="password"
            type={showPassword ? 'text' : 'password'}
            placeholder="Yeni şifrenizi girin"
            value={password}
            onChange={(e) => {
              setPassword(e.target.value)
              setFieldErrors((prev) => ({ ...prev, password: undefined }))
            }}
            className={cn('pr-9', fieldErrors.password && 'border-danger focus-visible:ring-danger')}
            autoComplete="new-password"
            aria-invalid={!!fieldErrors.password}
            disabled={loading}
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => setShowPassword((v) => !v)}
            className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7 text-text-tertiary hover:text-text-secondary hover:bg-transparent"
            aria-label={showPassword ? 'Şifreyi gizle' : 'Şifreyi göster'}
            tabIndex={-1}
          >
            {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
          </Button>
        </div>
        {fieldErrors.password ? (
          <p className="text-xs text-danger">{fieldErrors.password}</p>
        ) : (
          <p className="text-xs text-text-tertiary">{POLICY_HINT}</p>
        )}
      </div>

      <div className="space-y-1.5">
        <label htmlFor="confirm" className="block text-xs font-medium text-text-secondary">
          Şifreyi tekrarla
        </label>
        <div className="relative">
          <Input
            id="confirm"
            type={showConfirm ? 'text' : 'password'}
            placeholder="Şifrenizi tekrar girin"
            value={confirm}
            onChange={(e) => {
              setConfirm(e.target.value)
              setFieldErrors((prev) => ({ ...prev, confirm: undefined }))
            }}
            className={cn('pr-9', fieldErrors.confirm && 'border-danger focus-visible:ring-danger')}
            autoComplete="new-password"
            aria-invalid={!!fieldErrors.confirm}
            disabled={loading}
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            onClick={() => setShowConfirm((v) => !v)}
            className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7 text-text-tertiary hover:text-text-secondary hover:bg-transparent"
            aria-label={showConfirm ? 'Şifreyi gizle' : 'Şifreyi göster'}
            tabIndex={-1}
          >
            {showConfirm ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
          </Button>
        </div>
        {fieldErrors.confirm && (
          <p className="text-xs text-danger">{fieldErrors.confirm}</p>
        )}
      </div>

      <Button type="submit" className="w-full" disabled={loading}>
        {loading ? (
          <span className="flex items-center gap-2">
            <Spinner />
            Kaydediliyor...
          </span>
        ) : (
          'Şifreyi kaydet'
        )}
      </Button>
    </form>
  )
}
