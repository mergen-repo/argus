import * as React from 'react'
import { useNavigate } from 'react-router-dom'
import { CheckCircle, Circle } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { useOnboarding } from '@/hooks/use-onboarding'
import { cn } from '@/lib/utils'

interface ChecklistStep {
  key: string
  label: string
  href: string
}

const STEPS: ChecklistStep[] = [
  { key: 'operator_configured', label: 'Configure an operator', href: '/operators' },
  { key: 'apn_created', label: 'Create an APN', href: '/apns' },
  { key: 'sim_imported', label: 'Import your first SIMs', href: '/sims' },
  { key: 'policy_created', label: 'Create a policy', href: '/policies' },
]

export const FirstRunChecklist = React.memo(function FirstRunChecklist() {
  const navigate = useNavigate()
  const { data: status } = useOnboarding()

  if (!status) return null

  const completed = status as unknown as Record<string, boolean>
  const allDone = STEPS.every((s) => completed[s.key])

  if (allDone) return null

  const doneCount = STEPS.filter((s) => completed[s.key]).length

  return (
    <Card className="p-4 bg-bg-elevated border-border">
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold text-text-primary">Getting Started</h3>
        <span className="text-xs text-text-tertiary">{doneCount}/{STEPS.length} complete</span>
      </div>
      <div className="w-full h-1 bg-bg-hover rounded-full mb-3">
        <div
          className="h-full bg-accent rounded-full transition-all"
          style={{ width: `${(doneCount / STEPS.length) * 100}%` }}
        />
      </div>
      <ul className="flex flex-col gap-2">
        {STEPS.map((step) => {
          const done = !!completed[step.key]
          return (
            <li key={step.key} className="flex items-center gap-2">
              {done ? (
                <CheckCircle className="h-4 w-4 text-success flex-shrink-0" />
              ) : (
                <Circle className="h-4 w-4 text-text-tertiary flex-shrink-0" />
              )}
              {done ? (
                <span className="text-xs text-text-secondary line-through">{step.label}</span>
              ) : (
                <Button variant="link" className="h-auto p-0 text-xs text-accent" onClick={() => navigate(step.href)}>
                  {step.label}
                </Button>
              )}
            </li>
          )
        })}
      </ul>
    </Card>
  )
})
