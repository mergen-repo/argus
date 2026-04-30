import * as React from 'react'
import { ShieldAlert } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useImpersonation } from '@/hooks/use-impersonation'
import { cn } from '@/lib/utils'

interface ImpersonationBannerProps {
  className?: string
}

export const ImpersonationBanner = React.memo(function ImpersonationBanner({
  className,
}: ImpersonationBannerProps) {
  const { isImpersonating, exitImpersonation } = useImpersonation()

  if (!isImpersonating) return null

  return (
    <div
      className={cn(
        'flex items-center justify-between gap-3 px-4 py-2 text-sm',
        'bg-purple/10 border-b border-purple text-purple',
        className,
      )}
    >
      <span className="flex items-center gap-2">
        <ShieldAlert className="h-4 w-4 flex-shrink-0" />
        <span className="text-xs font-medium">
          You are viewing as another user (read-only). All changes are blocked.
        </span>
      </span>
      <Button
        size="sm"
        variant="outline"
        className="h-7 text-xs border-purple text-purple hover:bg-purple/10"
        onClick={() => exitImpersonation.mutate()}
        disabled={exitImpersonation.isPending}
      >
        Exit
      </Button>
    </div>
  )
})
