import { useState } from 'react'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useDeviceBinding, useRePair } from '@/hooks/use-device-binding'
import { BoundIMEIPanel } from './bound-imei-panel'
import { IMEIHistoryPanel } from './imei-history-panel'
import { RePairDialog } from './re-pair-dialog'

interface DeviceBindingTabProps {
  simId: string
}

export function DeviceBindingTab({ simId }: DeviceBindingTabProps) {
  const binding = useDeviceBinding(simId)
  const rePair = useRePair(simId)
  const [confirmOpen, setConfirmOpen] = useState(false)

  if (binding.isLoading) {
    return (
      <div className="space-y-4">
        <Card>
          <CardContent className="p-4 space-y-3">
            <Skeleton className="h-6 w-1/3" />
            <Skeleton className="h-24 w-full" />
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4 space-y-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </CardContent>
        </Card>
      </div>
    )
  }

  if (binding.isError || !binding.data) {
    return (
      <Card>
        <CardContent
          role="alert"
          aria-live="polite"
          className="flex flex-col items-center justify-center gap-3 py-16 text-center"
        >
          <AlertCircle className="h-8 w-8 text-danger" />
          <div>
            <p className="text-sm font-semibold text-text-primary">
              Failed to load device binding
            </p>
            <p className="mt-1 text-xs text-text-secondary">
              The device-binding service did not respond. Try again in a moment.
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => binding.refetch()}
            className="gap-1.5"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            Retry
          </Button>
        </CardContent>
      </Card>
    )
  }

  const handleRePairConfirm = async () => {
    try {
      await rePair.mutateAsync()
      toast.success('Re-pair successful — pending next auth')
      setConfirmOpen(false)
    } catch {
      // surfaced by axios interceptor toast
    }
  }

  return (
    <div className="space-y-4">
      <BoundIMEIPanel
        binding={binding.data}
        onRePair={() => setConfirmOpen(true)}
        rePairPending={rePair.isPending}
      />

      <div className="border-t border-border-subtle" aria-hidden="true" />

      <IMEIHistoryPanel simId={simId} />

      <RePairDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        previousIMEI={binding.data.bound_imei}
        bindingMode={binding.data.binding_mode}
        onConfirm={handleRePairConfirm}
        pending={rePair.isPending}
      />
    </div>
  )
}
