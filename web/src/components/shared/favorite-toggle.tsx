import * as React from 'react'
import { Star } from 'lucide-react'
import { useUIStore } from '@/stores/ui'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Tooltip } from '@/components/ui/tooltip'

interface FavoriteToggleProps {
  type: string
  id: string
  label: string
  path: string
  className?: string
}

export const FavoriteToggle = React.memo(function FavoriteToggle({
  type,
  id,
  label,
  path,
  className,
}: FavoriteToggleProps) {
  const { favorites, toggleFavorite } = useUIStore()
  const isFavorite = favorites.some((f) => f.id === id)

  const handleClick = React.useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault()
      e.stopPropagation()
      toggleFavorite({ type, id, label, path })
    },
    [toggleFavorite, type, id, label, path],
  )

  return (
    <Tooltip content={isFavorite ? 'Remove from favorites' : 'Add to favorites'}>
      <Button
        variant="ghost"
        size="icon"
        onClick={handleClick}
        className={cn(
          'h-7 w-7 transition-colors',
          isFavorite
            ? 'text-warning hover:text-warning/80'
            : 'text-text-tertiary hover:text-warning',
          className,
        )}
        aria-label={isFavorite ? 'Remove from favorites' : 'Add to favorites'}
        aria-pressed={isFavorite}
      >
        <Star className={cn('h-4 w-4', isFavorite && 'fill-current')} />
      </Button>
    </Tooltip>
  )
})
