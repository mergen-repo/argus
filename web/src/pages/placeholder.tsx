import { useLocation } from 'react-router-dom'

interface PlaceholderPageProps {
  title: string
  screenId?: string
}

export function PlaceholderPage({ title, screenId }: PlaceholderPageProps) {
  const location = useLocation()

  return (
    <div className="flex flex-col items-center justify-center py-24">
      <div className="rounded-xl border border-border bg-bg-surface p-8 text-center shadow-[var(--shadow-card)]">
        <h1 className="mb-2 text-lg font-semibold text-text-primary">{title}</h1>
        {screenId && (
          <p className="mb-1 font-mono text-xs text-accent">{screenId}</p>
        )}
        <p className="font-mono text-sm text-text-tertiary">{location.pathname}</p>
      </div>
    </div>
  )
}
