import { useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useUIStore } from '@/stores/ui'

const NAV_MAP: Record<string, string> = {
  d: '/',
  s: '/sims',
  a: '/apns',
  o: '/operators',
  p: '/policies',
  j: '/jobs',
  u: '/audit',
  n: '/sessions',
  l: '/alerts',
  t: '/topology',
  r: '/reports',
  c: '/capacity',
}

export function useKeyboardNav() {
  const navigate = useNavigate()
  const { toggleSidebar, setCommandPaletteOpen, commandPaletteOpen } = useUIStore()
  const waitingForNav = useRef(false)
  const timeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement
      const isInput = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable

      if (e.key === '/' && !isInput && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        setCommandPaletteOpen(true)
        return
      }

      if (isInput) return

      if (e.key === 'g' && !e.ctrlKey && !e.metaKey) {
        waitingForNav.current = true
        clearTimeout(timeoutRef.current)
        timeoutRef.current = setTimeout(() => {
          waitingForNav.current = false
        }, 1000)
        return
      }

      if (waitingForNav.current) {
        waitingForNav.current = false
        clearTimeout(timeoutRef.current)
        const path = NAV_MAP[e.key]
        if (path) {
          e.preventDefault()
          navigate(path)
        }
        return
      }

      if (e.key === '[' && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        toggleSidebar()
      }
      if (e.key === ']' && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        toggleSidebar()
      }

      // List row navigation (j/k/Enter/x) — operates on elements with [data-row-index]
      const rows = Array.from(document.querySelectorAll<HTMLElement>('[data-row-index]'))
      if (rows.length > 0 && !e.ctrlKey && !e.metaKey) {
        const activeIdx = rows.findIndex((r) => r.getAttribute('data-row-active') === 'true')
        if (e.key === 'j') {
          e.preventDefault()
          const next = Math.min(rows.length - 1, activeIdx < 0 ? 0 : activeIdx + 1)
          rows.forEach((r) => r.removeAttribute('data-row-active'))
          rows[next]?.setAttribute('data-row-active', 'true')
          rows[next]?.scrollIntoView({ block: 'nearest' })
          return
        }
        if (e.key === 'k') {
          e.preventDefault()
          const prev = Math.max(0, activeIdx < 0 ? 0 : activeIdx - 1)
          rows.forEach((r) => r.removeAttribute('data-row-active'))
          rows[prev]?.setAttribute('data-row-active', 'true')
          rows[prev]?.scrollIntoView({ block: 'nearest' })
          return
        }
        if (e.key === 'Enter' && activeIdx >= 0) {
          const href = rows[activeIdx]?.getAttribute('data-href')
          if (href) {
            e.preventDefault()
            navigate(href)
            return
          }
        }
        if (e.key === 'x' && activeIdx >= 0) {
          const row = rows[activeIdx]
          if (row) {
            e.preventDefault()
            row.dispatchEvent(new CustomEvent('argus:row-toggle', { bubbles: true }))
            return
          }
        }
      }

      // Detail page shortcuts
      if (e.key === 'e' && !e.ctrlKey && !e.metaKey) {
        if (document.querySelector('[data-detail-page="true"]')) {
          e.preventDefault()
          document.dispatchEvent(new CustomEvent('argus:edit'))
          return
        }
      }
      if (e.key === 'Backspace' && !e.ctrlKey && !e.metaKey) {
        if (document.querySelector('[data-detail-page="true"]')) {
          e.preventDefault()
          navigate(-1)
          return
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('keydown', handleKeyDown)
      clearTimeout(timeoutRef.current)
    }
  }, [navigate, toggleSidebar, setCommandPaletteOpen, commandPaletteOpen])
}
