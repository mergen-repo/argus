// FIX-239 DEV-544: Section 9 — Business Rules / Protocol Reference.
//
// Preserves the entire content of the legacy KB page (Standard Ports, SIM
// Authentication Flow, Session Lifecycle recap, Security Mechanisms, APN
// Types, CoA/DM) as the deep-dive reference home (AC-11: no content loss).
// The operational sections (1-8) are the primary entry points; this is the
// "encyclopedia" tab.

import { ArrowRight, ArrowLeftRight, Zap, Library } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'business-rules-reference',
  number: 9,
  title: 'Business Rules / Protocol Reference',
  subtitle: 'Deep-dive reference — every protocol detail, security mechanism, and APN model.',
  group: 'reference',
  icon: Library,
  searchTerms: ['protocol', 'reference', 'rfc 2865', 'rfc 5176', 'eap', 'ports', 'qos', 'security'],
  lastUpdated: '2026-04-27',
}

const PORTS_REF = [
  { protocol: 'RADIUS Auth', port: '1812/UDP', standard: 'RFC 2865', direction: 'inbound' as const, label: 'Operator → Argus' },
  { protocol: 'RADIUS Accounting', port: '1813/UDP', standard: 'RFC 2866', direction: 'inbound' as const, label: 'Operator → Argus' },
  { protocol: 'RADIUS CoA/DM', port: '3799/UDP', standard: 'RFC 5176', direction: 'outbound' as const, label: 'Argus → Operator' },
  { protocol: 'Diameter (Gx/Gy)', port: '3868/TCP', standard: 'RFC 6733', direction: 'bidirectional' as const, label: 'Bidirectional' },
  { protocol: '5G SBA (AUSF/UDM)', port: '8443/HTTPS', standard: '3GPP TS 29.509', direction: 'bidirectional' as const, label: 'Bidirectional' },
]

function DirectionBadge({ direction, label }: { direction: 'inbound' | 'outbound' | 'bidirectional'; label: string }) {
  if (direction === 'inbound') return <Badge variant="success">{label}</Badge>
  if (direction === 'outbound') return <Badge className="border-transparent bg-accent-dim text-accent">{label}</Badge>
  return <Badge className="border-transparent bg-warning-dim text-warning">{label}</Badge>
}

export function Component() {
  return (
    <div className="space-y-8">
      <Sub heading="Standard ports">
        <div className="overflow-x-auto rounded-[var(--radius-sm)] border border-border-subtle">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Protocol</TableHead>
                <TableHead>Port</TableHead>
                <TableHead>Standard</TableHead>
                <TableHead>Direction</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {PORTS_REF.map((row) => (
                <TableRow key={row.port}>
                  <TableCell><span className="font-medium text-text-primary">{row.protocol}</span></TableCell>
                  <TableCell><span className="font-mono text-xs text-text-secondary">{row.port}</span></TableCell>
                  <TableCell><span className="font-mono text-xs text-text-tertiary">{row.standard}</span></TableCell>
                  <TableCell><DirectionBadge direction={row.direction} label={row.label} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Sub>

      <Sub heading="SIM authentication flow (recap)">
        <div className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-elevated p-4 font-mono text-xs leading-relaxed overflow-x-auto">
          <div className="space-y-1 text-text-secondary min-w-[540px]">
            <div className="flex items-center gap-2">
              <span className="text-text-tertiary w-28 shrink-0">Device (SIM)</span>
              <ArrowRight className="h-3 w-3 text-accent shrink-0" aria-hidden="true" />
              <span className="text-text-tertiary">Base Station</span>
              <ArrowRight className="h-3 w-3 text-accent shrink-0" aria-hidden="true" />
              <span className="text-text-tertiary">Operator Core (P-GW)</span>
              <ArrowRight className="h-3 w-3 text-accent shrink-0" aria-hidden="true" />
              <span className="text-accent font-semibold">Argus RADIUS (:1812)</span>
            </div>
            <div className="mt-3 ml-[calc(28px+7rem+0.5rem)] border-l-2 border-accent/30 pl-4 space-y-1">
              <div className="text-text-tertiary">│</div>
              <div><span className="text-success">1.</span> SIM Lookup <span className="text-text-tertiary">(Redis cache → PostgreSQL fallback)</span></div>
              <div><span className="text-success">2.</span> State Check <span className="text-text-tertiary">(ACTIVE? SUSPENDED? TERMINATED?)</span></div>
              <div><span className="text-success">3.</span> APN Validation <span className="text-text-tertiary">(allowed APNs for this SIM)</span></div>
              <div><span className="text-success">4.</span> Policy Evaluation <span className="text-text-tertiary">(QoS, rate limits, time windows)</span></div>
              <div><span className="text-success">5.</span> IP Allocation <span className="text-text-tertiary">(from assigned IP pool)</span></div>
              <div className="text-text-tertiary">│</div>
              <div className="text-accent">Access-Accept + IP + QoS attributes</div>
            </div>
          </div>
        </div>
      </Sub>

      <Sub heading="Security mechanisms">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <SecCard
            title="EAP-SIM / EAP-AKA"
            tone="accent"
            body="Challenge-response authentication using SIM card cryptographic primitives. The network sends a random challenge (RAND); the SIM derives a session key using its Ki. Argus verifies the SRES response without ever transmitting the secret key."
          />
          <SecCard
            title="Concurrent Session Control"
            tone="warning"
            body="Each SIM profile defines a max concurrent session limit. On Access-Request, active session count is checked atomically via Redis. Excess sessions receive Access-Reject with Termination-Action=RADIUS, and a CoA is sent to terminate the oldest session."
          />
          <SecCard
            title="Anomaly Detection"
            tone="danger"
            body="Statistical models run on 5-minute CDR windows. Sudden spikes in auth failures, unusual data volumes, or SIM roaming outside declared regions trigger alerts. Persistent anomalies can auto-suspend the SIM via policy action."
          />
        </div>
      </Sub>

      <Sub heading="CoA / DM mid-session control">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <CoaPanel
            icon={<ArrowLeftRight className="h-4 w-4 text-accent" />}
            title="Change of Authorization (CoA)"
            rfc="RFC 5176"
            body="Argus sends a CoA-Request to the operator NAS (port 3799) to modify session attributes mid-flight. Used to dynamically change QoS class, rate limit, or APN parameters without dropping the session."
            triggers={['Policy rule change applied to live SIM', 'Data cap threshold reached — throttle to 64kbps', 'Roaming zone change — update QoS class']}
            triggerColor="text-accent"
          />
          <CoaPanel
            icon={<Zap className="h-4 w-4 text-danger" />}
            title="Disconnect Message (DM)"
            rfc="RFC 5176"
            body="Argus sends a Disconnect-Request to immediately terminate an active session. The NAS responds with Disconnect-ACK on success or Disconnect-NAK with an error code. The session record is closed and the IP returned to the pool."
            triggers={['SIM suspended or terminated via API', 'Anomaly detection — suspected SIM cloning', 'Admin-initiated bulk session reset']}
            triggerColor="text-danger"
          />
        </div>
      </Sub>
    </div>
  )
}

function Sub({ heading, children }: { heading: string; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="text-[12px] font-semibold text-text-primary mb-3">{heading}</h3>
      {children}
    </div>
  )
}

function SecCard({ title, tone, body }: { title: string; tone: 'accent' | 'warning' | 'danger'; body: string }) {
  const dotColor = tone === 'accent' ? 'bg-accent' : tone === 'warning' ? 'bg-warning' : 'bg-danger'
  return (
    <div className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-elevated p-4">
      <div className="flex items-center gap-2 mb-2">
        <div className={`h-2 w-2 rounded-full ${dotColor}`} aria-hidden="true" />
        <span className="text-xs font-semibold text-text-primary">{title}</span>
      </div>
      <p className="text-xs text-text-secondary leading-relaxed">{body}</p>
    </div>
  )
}

interface CoaPanelProps {
  icon: React.ReactNode
  title: string
  rfc: string
  body: string
  triggers: string[]
  triggerColor: string
}

function CoaPanel({ icon, title, rfc, body, triggers, triggerColor }: CoaPanelProps) {
  return (
    <div className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-elevated p-4">
      <div className="flex items-center gap-2 mb-3">
        {icon}
        <span className="text-xs font-semibold text-text-primary">{title}</span>
        <span className="font-mono text-[10px] text-text-tertiary border border-border-subtle rounded px-1.5 py-0.5">{rfc}</span>
      </div>
      <p className="text-xs text-text-secondary leading-relaxed mb-3">{body}</p>
      <div className="space-y-1.5">
        <div className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider">Common triggers</div>
        <ul className="space-y-1">
          {triggers.map((t) => (
            <li key={t} className="flex items-start gap-1.5 text-xs text-text-secondary">
              <span className={`${triggerColor} mt-0.5`}>·</span>
              {t}
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
