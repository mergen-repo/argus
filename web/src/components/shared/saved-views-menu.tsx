import * as React from 'react'
import { BookMarked, Check, Star, Trash2, Plus } from 'lucide-react'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuLabel } from '@/components/ui/dropdown-menu'
import { Button, buttonVariants } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useSavedViews } from '@/hooks/use-saved-views'
import type { SavedView } from '@/hooks/use-saved-views'
import { cn } from '@/lib/utils'

interface SavedViewsMenuProps {
  page: string
  currentFilters?: Record<string, unknown>
  onApply?: (view: SavedView) => void
}

export const SavedViewsMenu = React.memo(function SavedViewsMenu({
  page,
  currentFilters,
  onApply,
}: SavedViewsMenuProps) {
  const { data: views = [], create, remove, setDefault } = useSavedViews(page)
  const [showCreate, setShowCreate] = React.useState(false)
  const [newName, setNewName] = React.useState('')

  const handleCreate = async () => {
    if (!newName.trim()) return
    await create.mutateAsync({ page, name: newName.trim(), filters_json: currentFilters ?? {}, is_default: false, shared: false })
    setShowCreate(false)
    setNewName('')
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger className={buttonVariants({ variant: 'outline', size: 'sm' }) + ' gap-1.5 h-8'}>
          <BookMarked className="h-3.5 w-3.5" />
          Views
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-52">
          <DropdownMenuLabel className="text-[10px] uppercase tracking-wider text-text-tertiary font-normal">
            Saved Views
          </DropdownMenuLabel>
          {views.length === 0 && (
            <DropdownMenuItem disabled className="text-text-tertiary text-xs">No saved views</DropdownMenuItem>
          )}
          {views.map((v) => (
            <DropdownMenuItem
              key={v.id}
              className="flex items-center justify-between gap-2 group"
              onClick={() => onApply?.(v)}
            >
              <span className="flex items-center gap-1.5 truncate">
                {v.is_default && <Star className="h-3 w-3 text-warning flex-shrink-0" />}
                <span className="truncate text-sm">{v.name}</span>
              </span>
              <span className="flex gap-1 opacity-0 group-hover:opacity-100">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-5 w-5 p-0.5 text-text-tertiary hover:text-text-primary"
                  onClick={(e) => { e.stopPropagation(); setDefault.mutate(v.id) }}
                  title="Set as default"
                ><Check className="h-3 w-3" /></Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-5 w-5 p-0.5 text-text-tertiary hover:text-danger"
                  onClick={(e) => { e.stopPropagation(); remove.mutate(v.id) }}
                  title="Delete"
                ><Trash2 className="h-3 w-3" /></Button>
              </span>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setShowCreate(true)} className="gap-1.5 text-accent">
            <Plus className="h-3.5 w-3.5" />
            Save current view
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={showCreate} onOpenChange={setShowCreate}>
        <DialogContent className="sm:max-w-xs">
          <DialogHeader>
            <DialogTitle>Save View</DialogTitle>
          </DialogHeader>
          <Input
            placeholder="View name"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
            autoFocus
          />
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setShowCreate(false)}>Cancel</Button>
            <Button size="sm" onClick={handleCreate} disabled={!newName.trim() || create.isPending}>
              {create.isPending ? 'Saving…' : 'Save'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
})
