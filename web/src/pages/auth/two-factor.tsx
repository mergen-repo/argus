import { useState, useRef, useEffect, useCallback, type KeyboardEvent, type ClipboardEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Download, Copy, Check, Shield, AlertCircle, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/lib/api'

const CODE_LENGTH = 6
const CONFIRM_DELAY_MS = 5000

function normalizeBackupCode(raw: string): string {
  return raw.replace(/-/g, '').toUpperCase()
}

export default function TwoFactorPage() {
  const navigate = useNavigate()
  const partialToken = useAuthStore((s) => s.partialToken)
  const setAuth = useAuthStore((s) => s.setAuth)
  const clear2FA = useAuthStore((s) => s.clear2FA)

  const [digits, setDigits] = useState<string[]>(Array(CODE_LENGTH).fill(''))
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const inputRefs = useRef<(HTMLInputElement | null)[]>([])

  const [useBackupCode, setUseBackupCode] = useState(false)
  const [backupCodeInput, setBackupCodeInput] = useState('')
  const [backupCodesRemaining, setBackupCodesRemaining] = useState<number | null>(null)
  const backupInputRef = useRef<HTMLInputElement | null>(null)

  const [showBackupModal, setShowBackupModal] = useState(false)
  const [generatedCodes, setGeneratedCodes] = useState<string[]>([])
  const [generatingCodes, setGeneratingCodes] = useState(false)
  const [copied, setCopied] = useState(false)
  const [confirmEnabled, setConfirmEnabled] = useState(false)
  const [pendingAuth, setPendingAuth] = useState<{ user: ReturnType<typeof useAuthStore.getState>['user']; token: string } | null>(null)

  useEffect(() => {
    if (!partialToken) {
      navigate('/login', { replace: true })
    }
  }, [partialToken, navigate])

  useEffect(() => {
    if (useBackupCode) {
      backupInputRef.current?.focus()
    } else {
      inputRefs.current[0]?.focus()
    }
  }, [useBackupCode])

  useEffect(() => {
    if (!useBackupCode) {
      inputRefs.current[0]?.focus()
    }
  }, [])

  useEffect(() => {
    if (!showBackupModal) {
      setConfirmEnabled(false)
      return
    }
    const timer = setTimeout(() => setConfirmEnabled(true), CONFIRM_DELAY_MS)
    return () => clearTimeout(timer)
  }, [showBackupModal])

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
      submitTOTP(code)
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
      submitTOTP(pasted)
    } else {
      inputRefs.current[pasted.length]?.focus()
    }
  }

  const openBackupCodesModal = useCallback(async (user: ReturnType<typeof useAuthStore.getState>['user'], token: string) => {
    setPendingAuth({ user, token })
    setGeneratingCodes(true)
    try {
      const res = await authApi.generateBackupCodes()
      const codes = res.data.data.codes ?? []
      setGeneratedCodes(codes)
      setShowBackupModal(true)
    } catch {
      setAuth(user!, token)
      navigate('/', { replace: true })
    } finally {
      setGeneratingCodes(false)
    }
  }, [setAuth, navigate])

  async function submitTOTP(code: string) {
    if (loading) return
    setLoading(true)
    setError(null)

    try {
      const res = await authApi.verify2FA(code, undefined)
      const remaining = res.data.meta?.backup_codes_remaining
      if (remaining !== undefined) {
        setBackupCodesRemaining(remaining)
      }
      const token = res.data.data.token
      const user = useAuthStore.getState().user!

      if (remaining === 0) {
        await openBackupCodesModal(user, token)
      } else {
        setAuth(user, token)
        navigate('/', { replace: true })
      }
    } catch (err: unknown) {
      const axiosError = err as { response?: { data?: { error?: { message?: string } } } }
      setError(axiosError.response?.data?.error?.message || 'Invalid code. Please try again.')
      setDigits(Array(CODE_LENGTH).fill(''))
      inputRefs.current[0]?.focus()
    } finally {
      setLoading(false)
    }
  }

  async function submitBackupCode() {
    if (loading) return
    const normalized = normalizeBackupCode(backupCodeInput)
    if (normalized.length < 8) {
      setError('Backup code must be 8 characters (e.g. ABCD-EFGH or ABCDEFGH)')
      return
    }
    setLoading(true)
    setError(null)

    try {
      const res = await authApi.verify2FA(undefined, normalized)
      const remaining = res.data.meta?.backup_codes_remaining
      if (remaining !== undefined) {
        setBackupCodesRemaining(remaining)
      }
      const token = res.data.data.token
      const user = useAuthStore.getState().user!
      setAuth(user, token)
      navigate('/', { replace: true })
    } catch (err: unknown) {
      const axiosError = err as { response?: { data?: { error?: { message?: string } } } }
      setError(axiosError.response?.data?.error?.message || 'Invalid backup code. Please try again.')
      setBackupCodeInput('')
      backupInputRef.current?.focus()
    } finally {
      setLoading(false)
    }
  }

  function handleCancel() {
    clear2FA()
    navigate('/login', { replace: true })
  }

  function toggleMode() {
    setUseBackupCode((prev) => !prev)
    setError(null)
    setDigits(Array(CODE_LENGTH).fill(''))
    setBackupCodeInput('')
  }

  function handleCopyAll() {
    navigator.clipboard.writeText(generatedCodes.join('\n'))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  function handleDownload() {
    const content = [
      'Argus Backup Codes',
      '==================',
      'Keep these codes safe. Each code can only be used once.',
      '',
      ...generatedCodes,
      '',
      `Generated: ${new Date().toISOString()}`,
    ].join('\n')
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'argus-backup-codes.txt'
    a.click()
    URL.revokeObjectURL(url)
  }

  function handleConfirmSaved() {
    if (!pendingAuth) return
    setAuth(pendingAuth.user!, pendingAuth.token)
    setShowBackupModal(false)
    navigate('/', { replace: true })
  }

  if (!partialToken) return null

  return (
    <>
      <div className="space-y-4">
        <div className="mb-2 text-center">
          <h2 className="text-[15px] font-semibold text-text-primary">Two-Factor Authentication</h2>
          <p className="text-sm text-text-secondary">
            {useBackupCode
              ? 'Enter one of your recovery backup codes'
              : 'Enter the 6-digit code from your authenticator app'}
          </p>
        </div>

        {backupCodesRemaining !== null && backupCodesRemaining < 3 && (
          <div className="rounded-[var(--radius-sm)] border border-warning/30 bg-warning-dim px-3 py-2.5 text-sm">
            <p className="font-medium text-warning">
              Only {backupCodesRemaining} backup code{backupCodesRemaining !== 1 ? 's' : ''} remaining — regenerate in settings
            </p>
          </div>
        )}

        {error && (
          <div className="rounded-[var(--radius-sm)] border border-danger/30 bg-danger-dim px-3 py-2 text-sm text-danger text-center">
            {error}
          </div>
        )}

        {useBackupCode ? (
          <div className="space-y-2 py-2">
            <Input
              ref={backupInputRef}
              type="text"
              placeholder="ABCD-EFGH"
              value={backupCodeInput}
              onChange={(e) => {
                setBackupCodeInput(e.target.value)
                setError(null)
              }}
              disabled={loading}
              className="text-center font-mono text-base tracking-widest"
              maxLength={9}
              autoComplete="one-time-code"
              onKeyDown={(e) => {
                if (e.key === 'Enter') submitBackupCode()
              }}
            />
          </div>
        ) : (
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
                disabled={loading || generatingCodes}
                className="h-12 w-10 text-center font-mono text-lg"
                autoComplete="one-time-code"
              />
            ))}
          </div>
        )}

        <div className="flex flex-col gap-2">
          <Button
            type="button"
            disabled={loading || generatingCodes}
            className="w-full"
            onClick={() => {
              if (useBackupCode) {
                submitBackupCode()
              } else {
                const code = digits.join('')
                if (code.length === CODE_LENGTH) submitTOTP(code)
              }
            }}
          >
            {(loading || generatingCodes) ? (
              <span className="flex items-center gap-2">
                <Spinner />
                {generatingCodes ? 'Generating codes...' : 'Verifying...'}
              </span>
            ) : (
              'Verify'
            )}
          </Button>

          <Button
            type="button"
            variant="ghost"
            className="w-full text-text-secondary hover:text-text-primary"
            onClick={toggleMode}
            disabled={loading || generatingCodes}
          >
            {useBackupCode ? 'Use authenticator app instead' : 'Use a backup code instead'}
          </Button>

          <Button
            type="button"
            variant="ghost"
            className="w-full"
            onClick={handleCancel}
            disabled={loading || generatingCodes}
          >
            Back to login
          </Button>
        </div>
      </div>

      <Dialog open={showBackupModal} onOpenChange={() => {}}>
        <DialogContent
          style={{ maxWidth: '480px' }}
          onClick={(e) => e.stopPropagation()}
        >
          <DialogHeader>
            <div className="flex items-center gap-2 mb-1">
              <div className="flex h-8 w-8 items-center justify-center rounded-[var(--radius-sm)] bg-warning-dim border border-warning/30">
                <Shield className="h-4 w-4 text-warning" />
              </div>
              <DialogTitle>Save Your Backup Codes</DialogTitle>
            </div>
            <DialogDescription>
              Store these codes somewhere safe. Each code can only be used once if you lose access to your authenticator app.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="rounded-[var(--radius-md)] border border-border bg-bg-elevated p-4">
              <div className="grid grid-cols-2 gap-3">
                {generatedCodes.map((code, i) => (
                  <div
                    key={i}
                    className="flex items-center gap-2 rounded-[var(--radius-sm)] border border-border-subtle bg-bg-surface px-3 py-2"
                  >
                    <span className="text-[10px] text-text-tertiary w-4 shrink-0">{String(i + 1).padStart(2, '0')}</span>
                    <span className="font-mono text-sm text-text-primary tracking-wider">{code}</span>
                  </div>
                ))}
              </div>
            </div>

            <div className="flex items-center gap-2 rounded-[var(--radius-sm)] border border-warning/20 bg-warning-dim px-3 py-2">
              <AlertCircle className="h-3.5 w-3.5 text-warning shrink-0" />
              <p className="text-xs text-warning">These codes will not be shown again. Download or copy them now.</p>
            </div>

            <div className="flex gap-2">
              <Button variant="outline" size="sm" onClick={handleDownload} className="flex-1 gap-1.5">
                <Download className="h-3.5 w-3.5" />
                Download .txt
              </Button>
              <Button variant="outline" size="sm" onClick={handleCopyAll} className="flex-1 gap-1.5">
                {copied ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
                {copied ? 'Copied!' : 'Copy all'}
              </Button>
            </div>
          </div>

          <DialogFooter>
            <Button
              onClick={handleConfirmSaved}
              disabled={!confirmEnabled}
              className="w-full gap-2"
            >
              {!confirmEnabled ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  Please read the codes first...
                </>
              ) : (
                'I have saved my backup codes'
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
