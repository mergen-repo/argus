import { useEffect, useState } from 'react'
import { X, Command } from 'lucide-react'

interface Shortcut {
  keys: string[]
  description: string
}

interface ShortcutGroup {
  title: string
  shortcuts: Shortcut[]
}

const SHORTCUT_GROUPS: ShortcutGroup[] = [
  {
    title: 'NAVIGATION',
    shortcuts: [
      { keys: ['Ctrl', 'K'], description: 'Command palette' },
      { keys: ['G', 'D'], description: 'Go to Dashboard' },
      { keys: ['G', 'S'], description: 'Go to SIM Cards' },
      { keys: ['G', 'O'], description: 'Go to Operators' },
      { keys: ['G', 'P'], description: 'Go to Policies' },
      { keys: ['G', 'A'], description: 'Go to Alerts' },
      { keys: ['G', 'N'], description: 'Go to Sessions' },
    ],
  },
  {
    title: 'TABLES',
    shortcuts: [
      { keys: ['/'], description: 'Focus search' },
      { keys: ['J'], description: 'Next row' },
      { keys: ['K'], description: 'Previous row' },
      { keys: ['Enter'], description: 'Open detail' },
      { keys: ['X'], description: 'Toggle selection' },
      { keys: ['E'], description: 'Export data' },
    ],
  },
  {
    title: 'ACTIONS',
    shortcuts: [
      { keys: ['Ctrl', 'S'], description: 'Save (in editors)' },
      { keys: ['Ctrl', 'Enter'], description: 'Execute / Confirm' },
      { keys: ['Esc'], description: 'Close panel / Cancel' },
      { keys: ['?'], description: 'Show this help' },
    ],
  },
  {
    title: 'VIEWS',
    shortcuts: [
      { keys: ['['], description: 'Collapse sidebar' },
      { keys: [']'], description: 'Expand sidebar' },
      { keys: ['D'], description: 'Cycle table density' },
    ],
  },
]

function KeyboardShortcuts() {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === '?' && !e.ctrlKey && !e.metaKey && !e.altKey) {
        const target = e.target as HTMLElement
        if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) return
        e.preventDefault()
        setOpen((prev) => !prev)
      }
      if (e.key === 'Escape' && open) {
        setOpen(false)
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-[60]">
      <div className="fixed inset-0 bg-black/60 backdrop-blur-[2px] animate-fade-in" onClick={() => setOpen(false)} />
      <div className="fixed inset-0 flex items-center justify-center p-4">
        <div className="relative w-full max-w-2xl rounded-[var(--radius-lg)] border border-border bg-bg-surface shadow-2xl animate-slide-up-in overflow-hidden">
          <div className="flex items-center justify-between border-b border-border px-5 py-3">
            <div className="flex items-center gap-2">
              <Command className="h-4 w-4 text-accent" />
              <h2 className="text-[15px] font-semibold text-text-primary">Keyboard Shortcuts</h2>
            </div>
            <button onClick={() => setOpen(false)} className="rounded-md p-1.5 text-text-tertiary hover:text-text-primary hover:bg-bg-hover transition-colors">
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="p-5 grid grid-cols-2 gap-6 max-h-[70vh] overflow-y-auto">
            {SHORTCUT_GROUPS.map((group) => (
              <div key={group.title}>
                <h3 className="text-[10px] font-medium uppercase tracking-[1.5px] text-text-tertiary mb-3">{group.title}</h3>
                <div className="space-y-2">
                  {group.shortcuts.map((shortcut, idx) => (
                    <div key={idx} className="flex items-center justify-between">
                      <span className="text-xs text-text-secondary">{shortcut.description}</span>
                      <div className="flex items-center gap-1">
                        {shortcut.keys.map((key, kidx) => (
                          <span key={kidx}>
                            {kidx > 0 && <span className="text-text-tertiary text-[10px] mx-0.5">+</span>}
                            <kbd className="inline-flex h-5 min-w-[20px] items-center justify-center rounded border border-border bg-bg-elevated px-1.5 text-[10px] font-mono text-text-secondary">
                              {key}
                            </kbd>
                          </span>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
          <div className="border-t border-border px-5 py-2.5">
            <p className="text-[10px] text-text-tertiary text-center">
              Press <kbd className="inline-flex h-4 min-w-[16px] items-center justify-center rounded border border-border bg-bg-elevated px-1 text-[9px] font-mono text-text-secondary mx-0.5">?</kbd> to toggle this panel
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}

export { KeyboardShortcuts }
