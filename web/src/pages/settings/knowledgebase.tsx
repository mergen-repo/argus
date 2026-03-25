import {
  BookOpen,
  Server,
  GitBranch,
  RefreshCw,
  Shield,
  Network,
  Zap,
  ArrowRight,
  ArrowLeftRight,
} from 'lucide-react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const PORTS = [
  {
    protocol: 'RADIUS Auth',
    port: '1812/UDP',
    standard: 'RFC 2865',
    direction: 'inbound' as const,
    directionLabel: 'Operator → Argus',
  },
  {
    protocol: 'RADIUS Accounting',
    port: '1813/UDP',
    standard: 'RFC 2866',
    direction: 'inbound' as const,
    directionLabel: 'Operator → Argus',
  },
  {
    protocol: 'RADIUS CoA/DM',
    port: '3799/UDP',
    standard: 'RFC 5176',
    direction: 'outbound' as const,
    directionLabel: 'Argus → Operator',
  },
  {
    protocol: 'Diameter (Gx/Gy)',
    port: '3868/TCP',
    standard: 'RFC 6733',
    direction: 'bidirectional' as const,
    directionLabel: 'Bidirectional',
  },
  {
    protocol: '5G SBA (AUSF/UDM)',
    port: '8443/HTTPS',
    standard: '3GPP TS 29.509',
    direction: 'bidirectional' as const,
    directionLabel: 'Bidirectional',
  },
]

const LIFECYCLE_STEPS = [
  { step: 1, label: 'Auth Request', desc: 'Device sends Access-Request via operator RADIUS proxy' },
  { step: 2, label: 'SIM Lookup', desc: 'ICCID/IMSI resolved from Redis cache or PostgreSQL' },
  { step: 3, label: 'Accept / Reject', desc: 'State validated; ACTIVE proceeds, others get Access-Reject' },
  { step: 4, label: 'Accounting Start', desc: 'Acct-Status-Type=Start opens session record' },
  { step: 5, label: 'Interim Updates', desc: 'Periodic usage updates (Acct-Status-Type=Interim-Update)' },
  { step: 6, label: 'Session Stop', desc: 'Acct-Status-Type=Stop closes session, records final bytes' },
  { step: 7, label: 'IP Release', desc: 'Allocated IP returned to pool, CDR written to TimescaleDB' },
]

const APN_TYPES = [
  {
    type: 'private_managed',
    label: 'Private Managed',
    color: 'text-accent',
    bg: 'bg-accent-dim border-accent/20',
    desc: 'Argus fully controls the APN. IP allocation, QoS policies, and routing are managed internally. Suitable for closed enterprise deployments where Argus is the single source of truth.',
    useCase: 'Enterprise IoT fleets, closed M2M networks',
  },
  {
    type: 'operator_managed',
    label: 'Operator Managed',
    color: 'text-warning',
    bg: 'bg-warning-dim border-warning/20',
    desc: 'The mobile operator owns the APN configuration. Argus acts as a RADIUS AAA backend, handling authentication and policy only. IP assignment is delegated to the operator core.',
    useCase: 'MVNO deployments, operator wholesale agreements',
  },
  {
    type: 'customer_managed',
    label: 'Customer Managed',
    color: 'text-success',
    bg: 'bg-success-dim border-success/20',
    desc: 'The end customer owns and manages the APN. Argus provides authentication hooks and audit logging. Traffic routing and IP assignment happen outside the Argus control plane.',
    useCase: 'Large enterprise with dedicated APN infrastructure',
  },
]

function DirectionBadge({ direction, label }: { direction: 'inbound' | 'outbound' | 'bidirectional'; label: string }) {
  if (direction === 'inbound') {
    return <Badge variant="success">{label}</Badge>
  }
  if (direction === 'outbound') {
    return <Badge className="border-transparent bg-[color-mix(in_srgb,var(--color-accent)_15%,transparent)] text-accent">{label}</Badge>
  }
  return <Badge className="border-transparent bg-[color-mix(in_srgb,#a855f7_12%,transparent)] text-purple-400">{label}</Badge>
}

function SectionHeader({ icon: Icon, title, subtitle }: { icon: React.ElementType; title: string; subtitle?: string }) {
  return (
    <div className="flex items-start gap-3 mb-4">
      <div className="flex h-8 w-8 items-center justify-center rounded-md bg-accent-dim text-accent flex-shrink-0 mt-0.5">
        <Icon className="h-4 w-4" />
      </div>
      <div>
        <h2 className="text-[14px] font-semibold text-text-primary">{title}</h2>
        {subtitle && <p className="text-xs text-text-secondary mt-0.5">{subtitle}</p>}
      </div>
    </div>
  )
}

export default function KnowledgeBasePage() {
  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3 mb-2">
        <BookOpen className="h-5 w-5 text-accent" />
        <div>
          <h1 className="text-[16px] font-semibold text-text-primary">Knowledge Base</h1>
          <p className="text-xs text-text-secondary mt-0.5">AAA protocol reference for operator integration</p>
        </div>
      </div>

      {/* Section 1: Standard Ports */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            <SectionHeader
              icon={Server}
              title="Standard Ports"
              subtitle="All listeners on the Argus host. Ensure firewall rules are updated before pairing a new operator."
            />
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-0">
          <div className="overflow-x-auto">
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
                {PORTS.map((row) => (
                  <TableRow key={row.port}>
                    <TableCell>
                      <span className="font-medium text-text-primary">{row.protocol}</span>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-xs text-text-secondary">{row.port}</span>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-xs text-text-tertiary">{row.standard}</span>
                    </TableCell>
                    <TableCell>
                      <DirectionBadge direction={row.direction} label={row.directionLabel} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      {/* Section 2: SIM Authentication Flow */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            <SectionHeader
              icon={GitBranch}
              title="SIM Authentication Flow"
              subtitle="End-to-end path from device attach to IP assignment."
            />
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-0 space-y-4">
          <div className="rounded-lg border border-border bg-bg-elevated p-4 font-mono text-xs leading-relaxed overflow-x-auto">
            <div className="space-y-1 text-text-secondary min-w-[540px]">
              <div className="flex items-center gap-2">
                <span className="text-text-tertiary w-28 shrink-0">Device (SIM)</span>
                <ArrowRight className="h-3 w-3 text-accent shrink-0" />
                <span className="text-text-tertiary">Base Station</span>
                <ArrowRight className="h-3 w-3 text-accent shrink-0" />
                <span className="text-text-tertiary">Operator Core (P-GW)</span>
                <ArrowRight className="h-3 w-3 text-accent shrink-0" />
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
                <div className="text-text-tertiary">│</div>
              </div>
              <div className="mt-2 flex items-center gap-2">
                <span className="text-text-tertiary italic">Device gets IP, session starts</span>
                <span className="text-text-tertiary">◄───────────────────</span>
              </div>
            </div>
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div className="rounded-md border border-border bg-bg-elevated p-3">
              <div className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider mb-1">Cache Hit</div>
              <div className="text-xs text-text-secondary">ICCID resolved from Redis in &lt;1ms. No DB query needed for hot SIMs.</div>
            </div>
            <div className="rounded-md border border-border bg-bg-elevated p-3">
              <div className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider mb-1">Policy Engine</div>
              <div className="text-xs text-text-secondary">DSL rules evaluated in-memory. Reject reason logged for audit.</div>
            </div>
            <div className="rounded-md border border-border bg-bg-elevated p-3">
              <div className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider mb-1">IP Pools</div>
              <div className="text-xs text-text-secondary">Deterministic or random allocation from pool. Sticky option available.</div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Section 3: Session Lifecycle */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            <SectionHeader
              icon={RefreshCw}
              title="Session Lifecycle"
              subtitle="RADIUS accounting state machine from device attach to IP release."
            />
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-0">
          <div className="flex flex-wrap items-start gap-0">
            {LIFECYCLE_STEPS.map((s, i) => (
              <div key={s.step} className="flex items-start">
                <div className="flex flex-col items-center">
                  <div className="flex h-7 w-7 items-center justify-center rounded-full bg-accent-dim border border-accent/30 text-accent text-xs font-semibold">
                    {s.step}
                  </div>
                  <div className="mt-2 max-w-[110px] text-center">
                    <div className="text-xs font-medium text-text-primary leading-tight">{s.label}</div>
                    <div className="text-[10px] text-text-tertiary mt-0.5 leading-tight">{s.desc}</div>
                  </div>
                </div>
                {i < LIFECYCLE_STEPS.length - 1 && (
                  <ArrowRight className="h-4 w-4 text-border mt-1.5 mx-1 shrink-0" />
                )}
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      {/* Section 4: Security Mechanisms */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            <SectionHeader
              icon={Shield}
              title="Security Mechanisms"
              subtitle="Authentication and fraud prevention features built into the AAA engine."
            />
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-0 space-y-3">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <div className="rounded-lg border border-border bg-bg-elevated p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="h-2 w-2 rounded-full bg-accent" />
                <span className="text-xs font-semibold text-text-primary">EAP-SIM / EAP-AKA</span>
              </div>
              <p className="text-xs text-text-secondary leading-relaxed">
                Challenge-response authentication using SIM card cryptographic primitives. The network sends a random
                challenge (RAND); the SIM derives a session key using its Ki. Argus verifies the SRES response without
                ever transmitting the secret key.
              </p>
            </div>
            <div className="rounded-lg border border-border bg-bg-elevated p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="h-2 w-2 rounded-full bg-warning" />
                <span className="text-xs font-semibold text-text-primary">Concurrent Session Control</span>
              </div>
              <p className="text-xs text-text-secondary leading-relaxed">
                Each SIM profile defines a max concurrent session limit. On Access-Request, active session count is
                checked atomically via Redis. Excess sessions receive Access-Reject with Termination-Action=RADIUS,
                and a CoA is sent to terminate the oldest session.
              </p>
            </div>
            <div className="rounded-lg border border-border bg-bg-elevated p-4">
              <div className="flex items-center gap-2 mb-2">
                <div className="h-2 w-2 rounded-full bg-danger" />
                <span className="text-xs font-semibold text-text-primary">Anomaly Detection</span>
              </div>
              <p className="text-xs text-text-secondary leading-relaxed">
                Statistical models run on 5-minute CDR windows. Sudden spikes in auth failures, unusual data volumes,
                or SIM roaming outside declared regions trigger alerts. Persistent anomalies can auto-suspend the SIM
                via policy action.
              </p>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Section 5: APN Types */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            <SectionHeader
              icon={Network}
              title="APN Types"
              subtitle="Determines the ownership and control model for Access Point Name configuration."
            />
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-0 space-y-3">
          {APN_TYPES.map((apn) => (
            <div key={apn.type} className={`rounded-lg border p-4 ${apn.bg}`}>
              <div className="flex items-center gap-2 mb-2">
                <span className={`text-xs font-mono font-semibold ${apn.color}`}>{apn.type}</span>
                <span className="text-xs text-text-secondary">—</span>
                <span className="text-xs font-medium text-text-primary">{apn.label}</span>
              </div>
              <p className="text-xs text-text-secondary leading-relaxed mb-2">{apn.desc}</p>
              <div className="flex items-center gap-1.5">
                <span className="text-[10px] text-text-tertiary uppercase tracking-wider">Use case:</span>
                <span className="text-[10px] text-text-secondary">{apn.useCase}</span>
              </div>
            </div>
          ))}
        </CardContent>
      </Card>

      {/* Section 6: CoA/DM */}
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium">
            <SectionHeader
              icon={Zap}
              title="CoA / DM — Mid-Session Control"
              subtitle="RADIUS extensions that allow Argus to modify or terminate active sessions without waiting for natural expiry."
            />
          </CardTitle>
        </CardHeader>
        <CardContent className="pt-0 space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="rounded-lg border border-border bg-bg-elevated p-4">
              <div className="flex items-center gap-2 mb-3">
                <ArrowLeftRight className="h-4 w-4 text-accent" />
                <span className="text-xs font-semibold text-text-primary">Change of Authorization (CoA)</span>
                <span className="font-mono text-[10px] text-text-tertiary border border-border rounded px-1.5 py-0.5">RFC 5176</span>
              </div>
              <p className="text-xs text-text-secondary leading-relaxed mb-3">
                Argus sends a CoA-Request to the operator NAS (port 3799) to modify session attributes mid-flight.
                Used to dynamically change QoS class, rate limit, or APN parameters without dropping the session.
              </p>
              <div className="space-y-1.5">
                <div className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider">Common triggers</div>
                <ul className="space-y-1">
                  {['Policy rule change applied to live SIM', 'Data cap threshold reached — throttle to 64kbps', 'Roaming zone change — update QoS class'].map((t) => (
                    <li key={t} className="flex items-start gap-1.5 text-xs text-text-secondary">
                      <span className="text-accent mt-0.5">·</span>
                      {t}
                    </li>
                  ))}
                </ul>
              </div>
            </div>
            <div className="rounded-lg border border-border bg-bg-elevated p-4">
              <div className="flex items-center gap-2 mb-3">
                <Zap className="h-4 w-4 text-danger" />
                <span className="text-xs font-semibold text-text-primary">Disconnect Message (DM)</span>
                <span className="font-mono text-[10px] text-text-tertiary border border-border rounded px-1.5 py-0.5">RFC 5176</span>
              </div>
              <p className="text-xs text-text-secondary leading-relaxed mb-3">
                Argus sends a Disconnect-Request to immediately terminate an active session. The NAS responds with
                Disconnect-ACK on success or Disconnect-NAK with an error code. The session record is closed and
                the IP returned to the pool.
              </p>
              <div className="space-y-1.5">
                <div className="text-[10px] font-medium text-text-tertiary uppercase tracking-wider">Common triggers</div>
                <ul className="space-y-1">
                  {['SIM suspended or terminated via API', 'Anomaly detection — suspected SIM cloning', 'Admin-initiated bulk session reset'].map((t) => (
                    <li key={t} className="flex items-start gap-1.5 text-xs text-text-secondary">
                      <span className="text-danger mt-0.5">·</span>
                      {t}
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </div>
          <div className="rounded-md border border-border bg-bg-elevated p-3 font-mono text-xs text-text-secondary">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-[10px] text-text-tertiary uppercase tracking-wider">CoA Exchange</span>
            </div>
            <div className="space-y-0.5">
              <div><span className="text-accent">Argus</span> <ArrowRight className="inline h-3 w-3 text-text-tertiary" /> <span className="text-text-primary">CoA-Request</span> <span className="text-text-tertiary">[NAS-IP, Session-Id, new QoS attrs]</span> → <span className="text-text-secondary">NAS :3799</span></div>
              <div><span className="text-accent">Argus</span> <span className="text-text-tertiary">←</span> <span className="text-text-primary">CoA-ACK</span> <span className="text-text-tertiary">[Error-Cause=0 on success]</span> <span className="text-text-tertiary">←</span> <span className="text-text-secondary">NAS</span></div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
