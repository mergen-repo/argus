import { useState, useEffect, useCallback } from 'react'
import {
  Shield,
  RefreshCw,
  Download,
  Copy,
  Check,
  AlertCircle,
  Loader2,
  CheckCircle2,
  ShieldOff,
} from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { authApi } from '@/lib/api'

const CONFIRM_DELAY_MS = 5000

interface SecurityStatus {
  remaining: number
  totp_enabled: boolean
}

export default function SecurityPage() {
  const [status, setStatus] = useState<SecurityStatus | null>(null)
  const [loadingStatus, setLoadingStatus] = useState(true)

  const [showConfirmDialog, setShowConfirmDialog] = useState(false)
  const [showCodesModal, setShowCodesModal] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [codes, setCodes] = useState<string[]>([])
  const [copied, setCopied] = useState(false)
  const [confirmEnabled, setConfirmEnabled] = useState(false)

  const loadStatus = useCallback(async () => {
    setLoadingStatus(true)
    try {
      const res = await authApi.backupCodesRemaining()
      setStatus(res.data.data)
    } catch {
      setStatus(null)
    } finally {
      setLoadingStatus(false)
    }
  }, [])

  useEffect(() => {
    loadStatus()
  }, [loadStatus])

  useEffect(() => {
    if (!showCodesModal) {
      setConfirmEnabled(false)
      return
    }
    const timer = setTimeout(() => setConfirmEnabled(true), CONFIRM_DELAY_MS)
    return () => clearTimeout(timer)
  }, [showCodesModal])

  async function handleRegenerate() {
    setShowConfirmDialog(false)
    setGenerating(true)
    try {
      const res = await authApi.generateBackupCodes()
      const newCodes = res.data.data.codes ?? []
      setCodes(newCodes)
      setCopied(false)
      setShowCodesModal(true)
    } catch {
      // handled by api interceptor toast
    } finally {
      setGenerating(false)
    }
  }

  function handleCopyAll() {
    navigator.clipboard.writeText(codes.join('\n'))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  function handleDownload() {
    const content = [
      'Argus Backup Codes',
      '==================',
      'Keep these codes safe. Each code can only be used once.',
      '',
      ...codes,
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

  function handleDismissCodes() {
    setShowCodesModal(false)
    loadStatus()
  }

  function remainingVariant(n: number): 'success' | 'warning' | 'danger' {
    if (n >= 5) return 'success'
    if (n >= 2) return 'warning'
    return 'danger'
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between mb-2">
        <h1 className="text-[16px] font-semibold text-text-primary">Security</h1>
      </div>

      {/* 2FA Status */}
      <Card className="p-5">
        <div className="flex items-start justify-between">
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-[var(--radius-sm)] bg-accent-dim border border-accent/20">
              <Shield className="h-4.5 w-4.5 text-accent" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-text-primary">Two-Factor Authentication</h2>
              <p className="text-xs text-text-secondary mt-0.5">TOTP via authenticator app</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            {loadingStatus ? (
              <Loader2 className="h-4 w-4 animate-spin text-text-tertiary" />
            ) : status?.totp_enabled ? (
              <Badge variant="success" className="gap-1">
                <CheckCircle2 className="h-3 w-3" />
                Enabled
              </Badge>
            ) : (
              <Badge variant="secondary" className="gap-1">
                <ShieldOff className="h-3 w-3" />
                Disabled
              </Badge>
            )}
          </div>
        </div>

        {!loadingStatus && !status?.totp_enabled && (
          <div className="mt-4 flex items-center gap-2 rounded-[var(--radius-sm)] border border-warning/20 bg-warning-dim px-3 py-2">
            <AlertCircle className="h-3.5 w-3.5 text-warning shrink-0" />
            <p className="text-xs text-warning">
              Two-factor authentication is not enabled. Enable it in your account settings to protect your account.
            </p>
          </div>
        )}
      </Card>

      {/* Backup Codes */}
      <Card className="p-5">
        <div className="flex items-start justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-[var(--radius-sm)] bg-bg-elevated border border-border">
              <Shield className="h-4.5 w-4.5 text-text-secondary" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-text-primary">Backup Codes</h2>
              <p className="text-xs text-text-secondary mt-0.5">
                Emergency codes for account recovery when authenticator is unavailable
              </p>
            </div>
          </div>

          <div className="flex items-center gap-3 shrink-0">
            {!loadingStatus && status && (
              <div className="text-right">
                <Badge variant={remainingVariant(status.remaining)}>
                  {status.remaining} remaining
                </Badge>
              </div>
            )}
            <Button
              size="sm"
              variant="outline"
              className="gap-1.5"
              disabled={generating || loadingStatus || !status?.totp_enabled}
              onClick={() => setShowConfirmDialog(true)}
            >
              {generating ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  Generating...
                </>
              ) : (
                <>
                  <RefreshCw className="h-3.5 w-3.5" />
                  Regenerate
                </>
              )}
            </Button>
          </div>
        </div>

        {!loadingStatus && status?.totp_enabled && status.remaining < 3 && (
          <div className="mt-4 flex items-center gap-2 rounded-[var(--radius-sm)] border border-danger/20 bg-danger-dim px-3 py-2">
            <AlertCircle className="h-3.5 w-3.5 text-danger shrink-0" />
            <p className="text-xs text-danger">
              {status.remaining === 0
                ? 'You have no backup codes remaining. Regenerate them now to maintain account access.'
                : `Only ${status.remaining} backup code${status.remaining !== 1 ? 's' : ''} left. Consider regenerating soon.`}
            </p>
          </div>
        )}

        {!loadingStatus && !status?.totp_enabled && (
          <div className="mt-4 flex items-center gap-2 rounded-[var(--radius-sm)] border border-border bg-bg-elevated px-3 py-2">
            <AlertCircle className="h-3.5 w-3.5 text-text-tertiary shrink-0" />
            <p className="text-xs text-text-secondary">
              Enable two-factor authentication to generate backup codes.
            </p>
          </div>
        )}
      </Card>

      {/* Confirm Regenerate Dialog */}
      <Dialog open={showConfirmDialog} onOpenChange={setShowConfirmDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Regenerate Backup Codes?</DialogTitle>
            <DialogDescription>
              This will permanently invalidate all your current backup codes and generate 10 new ones. Any stored codes will no longer work.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowConfirmDialog(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleRegenerate} className="gap-2">
              <RefreshCw className="h-3.5 w-3.5" />
              Regenerate Codes
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* New Codes Modal */}
      <Dialog open={showCodesModal} onOpenChange={() => {}}>
        <DialogContent
          style={{ maxWidth: '480px' }}
          onClick={(e) => e.stopPropagation()}
        >
          <DialogHeader>
            <div className="flex items-center gap-2 mb-1">
              <div className="flex h-8 w-8 items-center justify-center rounded-[var(--radius-sm)] bg-warning-dim border border-warning/30">
                <Shield className="h-4 w-4 text-warning" />
              </div>
              <DialogTitle>New Backup Codes</DialogTitle>
            </div>
            <DialogDescription>
              Your previous codes have been invalidated. Save these new codes in a secure location — they will not be shown again.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="rounded-[var(--radius-md)] border border-border bg-bg-elevated p-4">
              <div className="grid grid-cols-2 gap-3">
                {codes.map((code, i) => (
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
              onClick={handleDismissCodes}
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
    </div>
  )
}
