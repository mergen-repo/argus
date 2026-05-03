import * as React from 'react'
import { AlertCircle, Loader2, Search } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import { isValidIMEI, IMEI_LENGTH } from '@/types/imei-lookup'

interface IMEILookupModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /**
   * Called with a validated 15-digit IMEI when the user submits the form.
   */
  onSubmit: (imei: string) => void
  /**
   * Optional initial IMEI value (e.g., when re-opening with a previously
   * looked-up IMEI). The input always starts editable.
   */
  initialImei?: string
  /**
   * When true, the submit button shows a spinner — typically while the
   * lookup query is in flight.
   */
  loading?: boolean
  /**
   * Server-side error to display inline (e.g., 422 INVALID_IMEI envelope
   * message). Cleared automatically when the user edits the input.
   */
  serverError?: string | null
}

export function IMEILookupModal({
  open,
  onOpenChange,
  onSubmit,
  initialImei,
  loading,
  serverError,
}: IMEILookupModalProps) {
  const [value, setValue] = React.useState(initialImei ?? '')
  const [touched, setTouched] = React.useState(false)
  const inputRef = React.useRef<HTMLInputElement>(null)
  const titleId = React.useId()
  const descId = React.useId()
  const errorId = React.useId()

  React.useEffect(() => {
    if (open) {
      setValue(initialImei ?? '')
      setTouched(false)
      const timer = window.setTimeout(() => inputRef.current?.focus(), 50)
      return () => window.clearTimeout(timer)
    }
    return undefined
  }, [open, initialImei])

  const trimmed = value.trim()
  const valid = isValidIMEI(trimmed)
  const showClientError = touched && trimmed.length > 0 && !valid
  const showServerError = !!serverError && !showClientError

  const clientErrorMessage =
    trimmed.length === 0
      ? 'Enter a 15-digit IMEI.'
      : !/^\d+$/.test(trimmed)
        ? 'IMEI must contain only digits.'
        : `Enter a ${IMEI_LENGTH}-digit IMEI (you entered ${trimmed.length}).`

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setValue(e.target.value.replace(/\s+/g, ''))
  }

  const handleSubmit = (e?: React.FormEvent) => {
    e?.preventDefault()
    setTouched(true)
    if (!valid || loading) return
    onSubmit(trimmed)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        onClose={() => onOpenChange(false)}
        aria-labelledby={titleId}
        aria-describedby={descId}
        className="w-[28rem]"
      >
        <DialogHeader>
          <div className="flex items-center gap-2">
            <span className="inline-flex h-8 w-8 items-center justify-center rounded-[var(--radius-sm)] bg-accent-dim text-accent">
              <Search className="h-4 w-4" />
            </span>
            <div>
              <DialogTitle id={titleId}>IMEI Lookup</DialogTitle>
              <DialogDescription id={descId}>
                Look up an IMEI across all device pools and bound SIMs in this tenant.
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-3">
          <div className="space-y-1.5">
            <label
              htmlFor={`${titleId}-imei-input`}
              className="text-xs font-medium uppercase tracking-wider text-text-tertiary"
            >
              IMEI (15 digits)
            </label>
            <Input
              id={`${titleId}-imei-input`}
              ref={inputRef}
              value={value}
              onChange={handleChange}
              onBlur={() => setTouched(true)}
              placeholder="359211089765432"
              inputMode="numeric"
              autoComplete="off"
              maxLength={IMEI_LENGTH}
              aria-invalid={showClientError || showServerError}
              aria-describedby={showClientError || showServerError ? errorId : undefined}
              className={cn(
                'font-mono text-sm tracking-wide',
                (showClientError || showServerError) &&
                  'border-danger focus-visible:ring-danger focus-visible:border-danger',
              )}
            />
            <p className="text-[11px] text-text-tertiary">
              Cross-tenant lookup is restricted to your tenant scope.
            </p>
            {(showClientError || showServerError) && (
              <p
                id={errorId}
                role="alert"
                className="flex items-start gap-1.5 text-xs text-danger"
              >
                <AlertCircle className="h-3.5 w-3.5 mt-0.5 shrink-0" />
                <span>{showServerError ? serverError : clientErrorMessage}</span>
              </p>
            )}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => onOpenChange(false)}
              disabled={loading}
            >
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={!valid || loading}>
              {loading ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  Looking up
                </>
              ) : (
                <>
                  <Search className="h-3.5 w-3.5" />
                  Look up
                </>
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
