import * as React from 'react'
import { Search } from 'lucide-react'
import { Button, type ButtonProps } from '@/components/ui/button'
import { IMEILookupModal } from './imei-lookup-modal'
import { IMEILookupDrawer } from './imei-lookup-drawer'
import { useIMEILookup } from '@/hooks/use-imei-lookup'

interface IMEILookupTriggerProps {
  /**
   * Visual variant for the toolbar button. Defaults to "outline" for
   * non-primary toolbar placement.
   */
  variant?: ButtonProps['variant']
  /**
   * Button size — defaults to "sm" to match existing toolbar buttons.
   */
  size?: ButtonProps['size']
  /**
   * Optional override label (defaults to "IMEI Lookup").
   */
  label?: string
  className?: string
}

/**
 * Cross-page IMEI Lookup entry point. Renders a toolbar button that opens
 * the IMEI input modal; on successful lookup, closes the modal and opens
 * the rich result drawer (Pool Membership + Bound SIMs + History).
 *
 * Used by SCR-196 (IMEI Pools), SCR-050 (Live Sessions), and SCR-020
 * (SIM List) toolbars.
 */
export function IMEILookupTrigger({
  variant = 'outline',
  size = 'sm',
  label = 'IMEI Lookup',
  className,
}: IMEILookupTriggerProps) {
  const [modalOpen, setModalOpen] = React.useState(false)
  const [drawerOpen, setDrawerOpen] = React.useState(false)
  const [imei, setImei] = React.useState('')
  const [serverError, setServerError] = React.useState<string | null>(null)

  const lookup = useIMEILookup(imei, { enabled: drawerOpen })

  // Surface server-side validation errors (e.g., 422 INVALID_IMEI) into the
  // modal as inline errors rather than destructive toasts. We re-open the
  // modal when a server error arrives so the user can correct input.
  React.useEffect(() => {
    if (!drawerOpen) return
    if (lookup.isError) {
      const err = lookup.error as Error & {
        response?: { data?: { error?: { code?: string; message?: string } } }
      }
      const apiErr = err?.response?.data?.error
      if (apiErr?.code === 'INVALID_IMEI') {
        setServerError(apiErr.message ?? 'Invalid IMEI format.')
        setDrawerOpen(false)
        setModalOpen(true)
      }
    }
  }, [lookup.isError, lookup.error, drawerOpen])

  const handleSubmit = (next: string) => {
    setServerError(null)
    setImei(next)
    setModalOpen(false)
    setDrawerOpen(true)
  }

  const handleModalChange = (open: boolean) => {
    setModalOpen(open)
    if (!open) setServerError(null)
  }

  const handleDrawerChange = (open: boolean) => {
    setDrawerOpen(open)
    if (!open) {
      // Drop cached IMEI so the next click re-opens a fresh modal session.
      setImei('')
    }
  }

  return (
    <>
      <Button
        type="button"
        variant={variant}
        size={size}
        className={className}
        onClick={() => {
          setModalOpen(true)
          setDrawerOpen(false)
        }}
      >
        <Search className="h-3.5 w-3.5" />
        {label}
      </Button>

      <IMEILookupModal
        open={modalOpen}
        onOpenChange={handleModalChange}
        onSubmit={handleSubmit}
        loading={false}
        serverError={serverError}
      />

      <IMEILookupDrawer
        open={drawerOpen}
        onOpenChange={handleDrawerChange}
        imei={imei}
        data={lookup.data}
        isLoading={lookup.isLoading || lookup.isFetching}
        isError={lookup.isError && !serverError}
        errorMessage={(lookup.error as Error | null)?.message ?? null}
        onRetry={() => lookup.refetch()}
      />
    </>
  )
}
