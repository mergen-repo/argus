import { Outlet } from 'react-router-dom'

export function AuthLayout() {
  return (
    <div className="flex min-h-screen items-center justify-center ambient-bg p-4">
      <div className="w-full max-w-md">
        <div className="mb-8 flex flex-col items-center">
          <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-lg bg-accent neon-glow text-bg-primary font-bold text-xl">
            A
          </div>
          <h1 className="text-xl font-semibold text-text-primary">Argus</h1>
          <p className="text-sm text-text-secondary">APN & Subscriber Intelligence Platform</p>
        </div>
        <div className="rounded-xl border border-border bg-bg-surface p-6 shadow-[var(--shadow-card)]">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
