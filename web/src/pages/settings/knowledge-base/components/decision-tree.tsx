// FIX-239 DEV-539: recursive decision-tree accordion for troubleshooting playbooks.
//
// Each node is either a question (with branches) or an action (with concrete
// fix steps + expected DB query / log pattern). Tree is rendered as nested
// disclosure widgets — keyboard-accessible by default.

import * as React from 'react'
import { ChevronRight, Wrench } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

export interface ActionNode {
  kind: 'action'
  title: string
  /** Steps the operator should take. */
  steps: string[]
  /** Optional SQL or shell to run — rendered as a code block. */
  query?: string
  /** Optional log pattern to grep. */
  logPattern?: string
}

export interface QuestionNode {
  kind: 'question'
  question: string
  branches: { label: string; child: TreeNode }[]
}

export type TreeNode = QuestionNode | ActionNode

interface DecisionTreeProps {
  root: TreeNode
}

export function DecisionTree({ root }: DecisionTreeProps) {
  return <TreeRenderer node={root} depth={0} />
}

function TreeRenderer({ node, depth }: { node: TreeNode; depth: number }) {
  if (node.kind === 'action') return <ActionCard action={node} />
  return <QuestionBlock node={node} depth={depth} />
}

function QuestionBlock({ node, depth }: { node: QuestionNode; depth: number }) {
  const [openIdx, setOpenIdx] = React.useState<number | null>(null)
  return (
    <div
      className={cn(
        'rounded-[var(--radius-sm)] border border-border-subtle',
        depth === 0 ? 'bg-bg-elevated' : 'bg-bg-surface',
      )}
    >
      <p className="text-xs font-medium text-text-primary px-3 py-2 border-b border-border-subtle">
        {node.question}
      </p>
      <ul className="divide-y divide-border-subtle">
        {node.branches.map((b, i) => {
          const isOpen = openIdx === i
          return (
            <li key={i}>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setOpenIdx(isOpen ? null : i)}
                aria-expanded={isOpen}
                className="flex w-full justify-start items-center gap-2 px-3 py-2 text-left text-xs text-text-secondary hover:bg-bg-hover/50 h-auto rounded-none"
              >
                <ChevronRight
                  className={cn('h-3.5 w-3.5 text-text-tertiary transition-transform', isOpen && 'rotate-90')}
                  aria-hidden="true"
                />
                <span>{b.label}</span>
              </Button>
              {isOpen && (
                <div className="px-3 pb-3 pl-7">
                  <TreeRenderer node={b.child} depth={depth + 1} />
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

function ActionCard({ action }: { action: ActionNode }) {
  return (
    <div className="rounded-[var(--radius-sm)] border border-success/30 bg-success-dim p-3 space-y-2">
      <div className="flex items-center gap-2">
        <Wrench className="h-3.5 w-3.5 text-success" />
        <span className="text-xs font-semibold text-text-primary">{action.title}</span>
      </div>
      <ol className="list-decimal pl-5 space-y-1 text-xs text-text-secondary">
        {action.steps.map((s, i) => (
          <li key={i}>{s}</li>
        ))}
      </ol>
      {action.query && (
        <div>
          <p className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Diagnostic query</p>
          <pre className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-primary px-3 py-2 text-[10px] font-mono text-text-primary overflow-x-auto whitespace-pre-wrap">
            {action.query}
          </pre>
        </div>
      )}
      {action.logPattern && (
        <div>
          <p className="text-[10px] uppercase tracking-wider text-text-tertiary mb-1">Log pattern</p>
          <pre className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-primary px-3 py-2 text-[10px] font-mono text-text-primary overflow-x-auto whitespace-pre-wrap">
            {action.logPattern}
          </pre>
        </div>
      )}
    </div>
  )
}
