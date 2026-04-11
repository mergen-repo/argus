import { useState, useRef, useEffect, type KeyboardEvent, type ClipboardEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/lib/api'

const CODE_LENGTH = 6

export default function TwoFactorPage() {
  const navigate = useNavigate()
  const partialToken = useAuthStore((s) => s.partialToken)
  const setAuth = useAuthStore((s) => s.setAuth)
  const clear2FA = useAuthStore((s) => s.clear2FA)

  const [digits, setDigits] = useState<string[]>(Array(CODE_LENGTH).fill(''))
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const inputRefs = useRef<(HTMLInputElement | null)[]>([])

  useEffect(() => {
    if (!partialToken) {
      navigate('/login', { replace: true })
    }
  }, [partialToken, navigate])

  useEffect(() => {
    inputRefs.current[0]?.focus()
  }, [])

  function handleChange(index: number, value: string) {
    if (!/^\d*$/.test(value)) return

    const newDigits = [...digits]
    newDigits[index] = value.slice(-1)
    setDigits(newDigits)
    setError(null)

    if (value && index < CODE_LENGTH - 1) {
      inputRefs.current[index + 1]?.focus()
    }

    const code = newDigits.join('')
    if (code.length === CODE_LENGTH && newDigits.every((d) => d !== '')) {
      submitCode(code)
    }
  }

  function handleKeyDown(index: number, e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Backspace' && !digits[index] && index > 0) {
      const newDigits = [...digits]
      newDigits[index - 1] = ''
      setDigits(newDigits)
      inputRefs.current[index - 1]?.focus()
    }
  }

  function handlePaste(e: ClipboardEvent<HTMLInputElement>) {
    e.preventDefault()
    const pasted = e.clipboardData.getData('text').replace(/\D/g, '').slice(0, CODE_LENGTH)
    if (!pasted) return

    const newDigits = Array(CODE_LENGTH).fill('')
    for (let i = 0; i < pasted.length; i++) {
      newDigits[i] = pasted[i]
    }
    setDigits(newDigits)

    if (pasted.length === CODE_LENGTH) {
      submitCode(pasted)
    } else {
      inputRefs.current[pasted.length]?.focus()
    }
  }

  async function submitCode(code: string) {
    if (loading) return
    setLoading(true)
    setError(null)

    try {
      const res = await authApi.verify2FA(code)
      const token = res.data.data.token
      const user = useAuthStore.getState().user!
      setAuth(user, token)
      navigate('/', { replace: true })
    } catch (err: unknown) {
      const axiosError = err as { response?: { data?: { error?: { message?: string } } } }
      setError(axiosError.response?.data?.error?.message || 'Invalid code. Please try again.')
      setDigits(Array(CODE_LENGTH).fill(''))
      inputRefs.current[0]?.focus()
    } finally {
      setLoading(false)
    }
  }

  function handleCancel() {
    clear2FA()
    navigate('/login', { replace: true })
  }

  if (!partialToken) return null

  return (
    <div className="space-y-4">
      <div className="mb-2 text-center">
        <h2 className="text-[15px] font-semibold text-text-primary">Two-Factor Authentication</h2>
        <p className="text-sm text-text-secondary">Enter the 6-digit code from your authenticator app</p>
      </div>

      {error && (
        <div className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-sm text-danger text-center">
          {error}
        </div>
      )}

      <div className="flex justify-center gap-2 py-2">
        {digits.map((digit, i) => (
          <Input
            key={i}
            ref={(el) => { inputRefs.current[i] = el }}
            type="text"
            inputMode="numeric"
            maxLength={1}
            value={digit}
            onChange={(e) => handleChange(i, e.target.value)}
            onKeyDown={(e) => handleKeyDown(i, e)}
            onPaste={i === 0 ? handlePaste : undefined}
            disabled={loading}
            className="h-12 w-10 text-center font-mono text-lg"
            autoComplete="one-time-code"
          />
        ))}
      </div>

      <div className="flex flex-col gap-2">
        <Button disabled={loading} className="w-full" onClick={() => {
          const code = digits.join('')
          if (code.length === CODE_LENGTH) submitCode(code)
        }}>
          {loading ? (
            <span className="flex items-center gap-2">
              <Spinner />
              Verifying...
            </span>
          ) : (
            'Verify'
          )}
        </Button>

        <Button
          type="button"
          variant="ghost"
          className="w-full"
          onClick={handleCancel}
          disabled={loading}
        >
          Back to login
        </Button>
      </div>
    </div>
  )
}
