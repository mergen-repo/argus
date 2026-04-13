import * as React from 'react'
import { Pencil, Check, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface EditableFieldProps {
  value: string
  onSave: (value: string) => Promise<void> | void
  disabled?: boolean
  className?: string
  inputClassName?: string
  label?: string
  validate?: (v: string) => string | undefined
}

export const EditableField = React.memo(function EditableField({
  value,
  onSave,
  disabled,
  className,
  inputClassName,
  label,
  validate,
}: EditableFieldProps) {
  const [editing, setEditing] = React.useState(false)
  const [draft, setDraft] = React.useState(value)
  const [saving, setSaving] = React.useState(false)
  const [error, setError] = React.useState<string | undefined>()
  const inputRef = React.useRef<HTMLInputElement>(null)

  React.useEffect(() => {
    if (editing) {
      setDraft(value)
      setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [editing, value])

  const handleSave = async () => {
    if (draft === value) { setEditing(false); return }
    if (validate) {
      const err = validate(draft)
      if (err) { setError(err); return }
    }
    setSaving(true)
    try {
      await onSave(draft)
      setEditing(false)
      setError(undefined)
    } catch {
      setError('Save failed')
    } finally {
      setSaving(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') handleSave()
    if (e.key === 'Escape') { setEditing(false); setDraft(value); setError(undefined) }
  }

  if (editing) {
    return (
      <span className={cn('inline-flex flex-col gap-1', className)}>
        <span className="inline-flex items-center gap-1">
          <Input
            ref={inputRef}
            value={draft}
            onChange={(e) => { setDraft(e.target.value); setError(undefined) }}
            onKeyDown={handleKeyDown}
            onBlur={handleSave}
            disabled={saving}
            className={cn('h-7 text-sm py-0', inputClassName)}
            aria-label={label ?? 'Edit field'}
          />
          <Button size="icon" variant="ghost" className="h-7 w-7" onClick={handleSave} disabled={saving}>
            <Check className="h-3.5 w-3.5 text-success" />
          </Button>
          <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => { setEditing(false); setDraft(value); setError(undefined) }}>
            <X className="h-3.5 w-3.5 text-text-tertiary" />
          </Button>
        </span>
        {error && <span className="text-xs text-danger">{error}</span>}
      </span>
    )
  }

  return (
    <span
      className={cn('group inline-flex items-center gap-1 cursor-default', !disabled && 'cursor-pointer', className)}
      onClick={() => !disabled && setEditing(true)}
    >
      <span>{value || <span className="text-text-tertiary italic">—</span>}</span>
      {!disabled && (
        <Pencil className="h-3 w-3 text-text-tertiary opacity-0 group-hover:opacity-100 transition-opacity" />
      )}
    </span>
  )
})
