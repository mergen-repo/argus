import { Loader2, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { BINDING_MODE_LABEL, type BindingMode } from '@/types/device-binding'

interface RePairDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  previousIMEI: string | null
  bindingMode: BindingMode | null
  onConfirm: () => void
  pending?: boolean
}

export function RePairDialog({
  open,
  onOpenChange,
  previousIMEI,
  bindingMode,
  onConfirm,
  pending,
}: RePairDialogProps) {
  const modeLabel = bindingMode ? BINDING_MODE_LABEL[bindingMode] : 'current binding mode'
  // Custom Dialog (not Radix) does not auto-wire aria-describedby; supply
  // explicit ids so screen-readers announce the description alongside the
  // title (WCAG 2.1.1 / 4.1.2 — confirm dialog must describe the action).
  const titleId = 'repair-dialog-title'
  const descriptionId = 'repair-dialog-description'
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        role="alertdialog"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
        onClose={() => onOpenChange(false)}
      >
        <DialogHeader>
          <DialogTitle id={titleId}>Re-pair this SIM to a new IMEI?</DialogTitle>
          <DialogDescription id={descriptionId}>
            This will clear the bound IMEI{' '}
            <span className="font-mono text-text-primary">
              {previousIMEI ? `'${previousIMEI}'` : '(none)'}
            </span>
            . The SIM will be{' '}
            <span className="font-mono text-text-primary">pending</span> until the next
            authentication, when it will lock to the observed IMEI per the current binding mode (
            <span className="font-mono text-text-primary">{modeLabel}</span>).
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={onConfirm}
            disabled={pending}
            className="gap-1.5"
          >
            {pending ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" />
            )}
            {pending ? 'Re-pairing…' : 'Re-pair'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
