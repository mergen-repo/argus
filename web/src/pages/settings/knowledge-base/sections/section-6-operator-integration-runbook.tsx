// FIX-239 DEV-543: Section 6 — Operator Integration Runbook.
//
// Heavy section — preserves the full PORTS table from the legacy KB page,
// adds RequestResponsePopup triggers per protocol, firewall snippets, and an
// integration test checklist.

import * as React from 'react'
import { Server, Play } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { RequestResponsePopup, type RequestResponseExample } from '../components/request-response-popup'
import type { SectionMeta } from '../types'

export const meta: SectionMeta = {
  id: 'operator-integration-runbook',
  number: 6,
  title: 'Operator Integration Runbook',
  subtitle: 'Standard ports, wire-format examples, firewall snippets, integration test checklist.',
  group: 'reference',
  icon: Server,
  searchTerms: ['port', 'firewall', 'iptables', 'security group', 'cloud armor', 'radius example', 'diameter example', '5g sba', 'integration test'],
  lastUpdated: '2026-04-27',
}

interface PortRow {
  protocol: string
  port: string
  standard: string
  direction: 'inbound' | 'outbound' | 'bidirectional'
  directionLabel: string
  example?: RequestResponseExample
}

const PORTS: PortRow[] = [
  {
    protocol: 'RADIUS Auth',
    port: '1812/UDP',
    standard: 'RFC 2865',
    direction: 'inbound',
    directionLabel: 'Operator → Argus',
    example: {
      title: 'RADIUS Access-Request',
      reference: 'RFC 2865 §4.1',
      wire: `Code: 1 (Access-Request)  Identifier: 0x42  Length: 86
Authenticator: 0xfe 22 81 ... (16 bytes)
Attributes:
  User-Name (1)        = "8990011199230012345"   (ICCID)
  NAS-IP-Address (4)   = 203.0.113.10
  NAS-Port (5)         = 12
  Service-Type (6)     = 2 (Framed-User)
  Called-Station-Id (30)= "iot.argus.io"          (APN)
  3GPP-IMSI (VSA 10415:1) = "286010000123456"`,
      curl: `echo "User-Name=8990011199230012345,NAS-IP-Address=203.0.113.10,NAS-Port=12,Service-Type=2,Called-Station-Id=iot.argus.io" \\
  | radclient -x argus-host:1812 auth $SHARED_SECRET`,
      response: `Code: 2 (Access-Accept)
Attributes:
  Framed-IP-Address (8) = 10.42.0.27
  Session-Timeout (27)  = 86400
  Acct-Interim-Interval (85) = 300
  3GPP-Charging-Id (VSA 10415:2) = 0xa2c41e90
  Reply-Message (18)    = "ok"`,
    },
  },
  {
    protocol: 'RADIUS Accounting',
    port: '1813/UDP',
    standard: 'RFC 2866',
    direction: 'inbound',
    directionLabel: 'Operator → Argus',
    example: {
      title: 'RADIUS Accounting-Request (Start)',
      reference: 'RFC 2866 §4',
      wire: `Code: 4 (Accounting-Request)  Identifier: 0x12
Attributes:
  Acct-Status-Type (40)  = 1 (Start)
  Acct-Session-Id (44)   = "1A2B3C4D"
  User-Name (1)          = "8990011199230012345"
  Framed-IP-Address (8)  = 10.42.0.27
  NAS-IP-Address (4)     = 203.0.113.10`,
      curl: `echo "Acct-Status-Type=1,Acct-Session-Id=1A2B3C4D,User-Name=...,Framed-IP-Address=10.42.0.27" \\
  | radclient -x argus-host:1813 acct $SHARED_SECRET`,
      response: `Code: 5 (Accounting-Response)
Attributes: (none — empty body confirms receipt)`,
    },
  },
  {
    protocol: 'RADIUS CoA / DM',
    port: '3799/UDP',
    standard: 'RFC 5176',
    direction: 'outbound',
    directionLabel: 'Argus → Operator',
    example: {
      title: 'RADIUS CoA-Request (Bandwidth change)',
      reference: 'RFC 5176 §3.1',
      wire: `Code: 43 (CoA-Request)
Attributes:
  Acct-Session-Id (44)         = "1A2B3C4D"
  NAS-IP-Address (4)           = 203.0.113.10
  Filter-Id (11)               = "qos:rate=64kbps"
  Service-Type (6)             = 2`,
      curl: `# Argus emits this; the NAS responds — there is no shell-tool from operator side.`,
      response: `Code: 44 (CoA-ACK)
Attributes:
  (none — empty body confirms application of new attributes)`,
    },
  },
  {
    protocol: 'Diameter (Gx/Gy)',
    port: '3868/TCP',
    standard: 'RFC 6733',
    direction: 'bidirectional',
    directionLabel: 'Bidirectional',
    example: {
      title: 'Diameter CCR-Initial (Gy / online charging)',
      reference: '3GPP TS 32.299',
      wire: `Command-Code: 272 (Credit-Control)  Application-Id: 4
AVPs:
  Session-Id (263)            = "argus.io;1234;567;1A2B"
  Origin-Host (264)           = "argus.argus.io"
  Origin-Realm (296)          = "argus.io"
  CC-Request-Type (416)       = 1 (INITIAL_REQUEST)
  CC-Request-Number (415)     = 0
  Service-Identifier (439)    = 1
  Subscription-Id (443) {
    Subscription-Id-Type (450) = 0 (E164)
    Subscription-Id-Data (444) = "905320001122"
  }
  Multiple-Services-Credit-Control (456) {
    Requested-Service-Unit (437) { CC-Total-Octets = 1048576 }
  }`,
      curl: `# Use a Diameter test client (e.g. seagull) — no curl equivalent.`,
      response: `CCA-Initial:
  Result-Code (268)            = 2001 (DIAMETER_SUCCESS)
  Multiple-Services-Credit-Control (456) {
    Granted-Service-Unit (431) { CC-Total-Octets = 1048576 }
    Validity-Time (448)        = 600
  }`,
    },
  },
  {
    protocol: '5G SBA (AUSF/UDM)',
    port: '8443/HTTPS',
    standard: '3GPP TS 29.509',
    direction: 'bidirectional',
    directionLabel: 'Bidirectional',
    example: {
      title: '5G SBA — Nausf_UEAuthentication_Authenticate',
      reference: '3GPP TS 29.509 §5.2.2.2',
      wire: `POST /nausf-auth/v1/ue-authentications HTTP/2
Content-Type: application/json
Authorization: Bearer <oauth-token>

{
  "supiOrSuci": "suci-0-286-01-0-0-0-0123456789",
  "servingNetworkName": "5G:mnc010:mcc286:nid:000000000000000000000000000000:00"
}`,
      curl: `curl -s --http2-prior-knowledge \\
  --cacert ca.pem --cert client.pem --key client.key \\
  -H "Authorization: Bearer $OAUTH_TOKEN" \\
  -H "Content-Type: application/json" \\
  -X POST https://argus-host:8443/nausf-auth/v1/ue-authentications \\
  -d '{"supiOrSuci":"suci-0-286-01-0-0-0-0123456789","servingNetworkName":"5G:mnc010:mcc286"}'`,
      response: `HTTP/2 201 Created
Content-Type: application/json
Location: /nausf-auth/v1/ue-authentications/abc123/5g-aka-confirmation

{
  "authType": "5G_AKA",
  "5gAuthData": {
    "rand": "...",
    "autn": "...",
    "hxresStar": "..."
  },
  "_links": {
    "5gAkaConfirmation": { "href": "/...confirmation" }
  }
}`,
    },
  },
]

function DirectionBadge({ direction, label }: { direction: PortRow['direction']; label: string }) {
  if (direction === 'inbound') return <Badge variant="success">{label}</Badge>
  if (direction === 'outbound') return (
    <Badge className="border-transparent bg-accent-dim text-accent">{label}</Badge>
  )
  return <Badge className="border-transparent bg-warning-dim text-warning">{label}</Badge>
}

export function Component() {
  const [active, setActive] = React.useState<RequestResponseExample | null>(null)

  return (
    <div className="space-y-6">
      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-3">Standard ports</h3>
        <div className="overflow-x-auto rounded-[var(--radius-sm)] border border-border-subtle">
          <Table>
            <TableHeader className="bg-bg-elevated">
              <TableRow>
                <TableHead>Protocol</TableHead>
                <TableHead>Port</TableHead>
                <TableHead>Standard</TableHead>
                <TableHead>Direction</TableHead>
                <TableHead className="print:hidden">Example</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {PORTS.map((row) => (
                <TableRow key={row.port}>
                  <TableCell><span className="font-medium text-text-primary text-xs">{row.protocol}</span></TableCell>
                  <TableCell><span className="font-mono text-xs text-text-secondary">{row.port}</span></TableCell>
                  <TableCell><span className="font-mono text-xs text-text-tertiary">{row.standard}</span></TableCell>
                  <TableCell>
                    <DirectionBadge direction={row.direction} label={row.directionLabel} />
                  </TableCell>
                  <TableCell className="print:hidden">
                    {row.example && (
                      <Button variant="outline" size="sm" className="gap-1.5 text-[11px]" onClick={() => setActive(row.example!)}>
                        <Play className="h-3 w-3" />
                        Wire format
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </div>

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-2">Firewall snippets</h3>
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-3">
          <FirewallSnippet
            title="iptables"
            body={`# Allow operator NAS to reach Argus AAA
iptables -A INPUT -s 203.0.113.0/24 -p udp --dport 1812 -j ACCEPT
iptables -A INPUT -s 203.0.113.0/24 -p udp --dport 1813 -j ACCEPT
# Allow Argus → operator NAS for CoA / DM
iptables -A OUTPUT -d 203.0.113.0/24 -p udp --dport 3799 -j ACCEPT`}
          />
          <FirewallSnippet
            title="AWS Security Group"
            body={`# Inbound rules
- Type: Custom UDP, Port: 1812, Source: 203.0.113.0/24
- Type: Custom UDP, Port: 1813, Source: 203.0.113.0/24
# Outbound rules
- Type: Custom UDP, Port: 3799, Destination: 203.0.113.0/24`}
          />
          <FirewallSnippet
            title="GCP Cloud Armor"
            body={`# Backend service: argus-aaa
gcloud compute firewall-rules create argus-radius-in \\
  --action=ALLOW --rules=udp:1812,udp:1813 \\
  --source-ranges=203.0.113.0/24
gcloud compute firewall-rules create argus-coa-out \\
  --direction=EGRESS --action=ALLOW --rules=udp:3799 \\
  --destination-ranges=203.0.113.0/24`}
          />
        </div>
      </div>

      <div>
        <h3 className="text-[12px] font-semibold text-text-primary mb-2">Integration test checklist</h3>
        <ol className="list-decimal pl-5 space-y-1 text-xs text-text-secondary">
          <li>Argus reachable from NAS — <code className="font-mono text-[11px]">nc -uvz argus-host 1812</code> succeeds.</li>
          <li>radclient Access-Request with a known fixture ICCID returns Access-Accept with IP, QoS, Session-Timeout.</li>
          <li>radclient Access-Request with an unknown ICCID returns Access-Reject with code <code className="font-mono">sim_not_found</code>.</li>
          <li>Acct-Start written within 5 seconds of Access-Accept; visible in <code className="font-mono">/sessions</code> page.</li>
          <li>Issue Disconnect from Argus UI — NAS terminates the session, Acct-Stop arrives, IP returned to pool.</li>
          <li>Audit log shows <code className="font-mono">auth.attempt</code>, <code className="font-mono">auth.accept</code>,{' '}
            <code className="font-mono">session.start</code>, <code className="font-mono">session.stop</code>,{' '}
            <code className="font-mono">coa.request</code>, <code className="font-mono">coa.ack</code>.</li>
        </ol>
      </div>

      {active && (
        <RequestResponsePopup
          open={!!active}
          onOpenChange={(open) => { if (!open) setActive(null) }}
          example={active}
        />
      )}
    </div>
  )
}

function FirewallSnippet({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-[var(--radius-sm)] border border-border-subtle bg-bg-elevated">
      <div className="border-b border-border-subtle px-3 py-1.5 text-[11px] font-semibold text-text-primary">
        {title}
      </div>
      <pre className="p-3 text-[10px] font-mono text-text-secondary leading-relaxed whitespace-pre-wrap break-all overflow-x-auto">
        {body}
      </pre>
    </div>
  )
}
