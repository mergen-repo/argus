# FIX-239: Knowledge Base — Ops Runbook Redesign (9 Sections + Interactive Request/Response Popups)

## Problem Statement
Current `/settings/knowledgebase` is a static single-page "AAA protocol reference" with 6 sections (Standard Ports, SIM Authentication Flow, Session Lifecycle, Security Mechanisms, APN Types, CoA/DM). Valuable content but narrow scope — ops/NOC/support team has no operational runbook for:
- How to onboard a new operator
- How to handle operator degradation
- How to troubleshoot policy not applying
- How to do common tasks (suspend SIM fleet, change bandwidth for all, etc.)

User preference (2026-04-19): **diagram-first, not text-heavy, clickable examples (popup with concrete request/response)**, existing 6 business-rule sections PRESERVED and integrated.

## User Story
As a NOC/ops engineer, I want a Knowledge Base organized around operational workflows (onboarding, business flows, troubleshooting, common operations) with diagrams and concrete API examples, so I can resolve issues without reading code or asking colleagues.

## Architecture Reference
- Current: Single JSX file `web/src/pages/settings/knowledgebase.tsx` — static content
- Target: Either MDX-based content (developer-edit) OR lightweight CMS-backed (non-technical edit). Chosen: **MDX** for first iteration — file-based, version-controlled, good balance.

## Findings Addressed
- F-230 (Knowledge Base genişletme + operasyonel runbook + interactive popup)

## Acceptance Criteria
- [ ] **AC-1:** **9-section structure:**
  1. **Operator Onboarding Flow** [NEW] — stepper: Create Operator → Protocols Config → Firewall Whitelist → Test Auth → Go Live
  2. **AAA Business Flow** — sequence diagram + Reject reasons cheatsheet + CoA in practice (integrates existing "SIM Authentication Flow" + "Security Mechanisms" + "CoA/DM Session Control")
  3. **Session Lifecycle** — timeline + stop reasons accordion (builds on existing)
  4. **Policy Workflow** [NEW] — DSL/Form → Preview → Dry Run → Canary 1% → Advance → Full rollout flowchart
  5. **IP Allocation + APN Types** — SIM attach → Pool lease → Session active → Detach diagram (integrates existing "APN Types")
  6. **Operator Integration Runbook** [NEW — heavy section] — Standard Ports reference + request/response popups + checklist + common failures troubleshoot
  7. **Common Operations Cookbook** [NEW] — "Suspend SIM fleet", "Add APN", "Reduce bandwidth", "Block lost SIM" how-tos
  8. **Troubleshooting Playbooks** [NEW] — decision tree for "Tüm SIM auth fail", "Policy uygulanmıyor", "Sessions stuck idle"
  9. **Business Rules / Protocol Reference** — preserves existing 6 sections as standalone reference (Standard Ports, Security Mechanisms, APN Types, CoA/DM) for detail lookup

- [ ] **AC-2:** **Layout:**
  - Left sticky TOC (9 sections)
  - Top Cmd+K fuzzy search (full-text)
  - Main card-based with accordion inside each section
  - Color coded: onboarding=blue, operations=green, troubleshooting=orange
  - Print-friendly: "Export runbook as PDF" for offline access (SRE acil durum)

- [ ] **AC-3:** **Interactive request/response popups** — each protocol row / operation clickable → SlidePanel (FIX-216 pattern) with:
  - Wire format view (hex dump, AVP breakdown)
  - curl example (radclient / raw HTTP)
  - Expected response
  - "Try in Live Tester" button (future — link to operator detail Test Connection)
  - Popup toggle "Show wire format" hides RFC-level detail for non-experts

- [ ] **AC-4:** **Content delivery — MDX:**
  - MDX files under `web/src/content/knowledge-base/*.mdx`
  - MDX renderer integrated in the KB page
  - Each MDX file supports React components (stepper, sequence diagram via Mermaid, code snippet, popup trigger)

- [ ] **AC-5:** **Diagram-first principle:**
  - Each section starts with a flowchart/sequence/timeline (SVG or Mermaid)
  - Text = short captions + expand accordion

- [ ] **AC-6:** **Existing 6 sections preservation:**
  - Standard Ports table → Section 6 + 9 (both places)
  - SIM Authentication Flow → Section 2 (primary) + references Section 9
  - Session Lifecycle → Section 3 (primary)
  - Security Mechanisms → Section 2 nested + Section 9
  - APN Types → Section 5 prefix
  - CoA/DM → Section 2 sub-section + Section 8 troubleshoot

- [ ] **AC-7:** **Operator Integration Runbook** includes:
  - Onboarding checklist — interactive (localStorage-persisted per-user progress)
  - Request/response popups for RADIUS Access-Request/Accept, Diameter Gx/Gy CCR/CCA, 5G SBA AUSF/UDM
  - Firewall config snippets (iptables, AWS Security Group, Cloud Armor)
  - Integration test playbook with expected responses

- [ ] **AC-8:** **Common Operations Cookbook** entries (each a how-to card):
  - Suspend SIM fleet (1000 SIMs bulk)
  - Add APN + assign fleet
  - Reduce bandwidth via policy rollout
  - Block lost SIM (state=stolen_lost + CoA disconnect)
  - Rotate API key
  - Investigate session drop spike
  - Handle operator outage (failover)

- [ ] **AC-9:** **Troubleshooting Playbooks** — decision tree cards:
  - "All SIMs auth failing" → checklist (operator health / shared secret / firewall / DB reachable / NATS up)
  - "Policy not applying to SIM" → version state → assignment check → CoA ack status (post-FIX-231 canonical)
  - "Sessions stuck idle > 1h" → interim update gap → NAS connectivity → SIM state check
  - Each item: expected DB query + expected log pattern + fix action

- [ ] **AC-10:** **Search + navigation:**
  - Cmd+K opens quick search — fuzzy match across all MDX content
  - Deep-link: `/settings/knowledgebase#operator-integration-radius` works

- [ ] **AC-11:** **No content loss:** Every piece of info from current 6 sections present in new structure.

- [ ] **AC-12:** **Versioning / updates:**
  - MDX files committed to git — change tracking via PR review
  - Release notes mention KB updates when sections change
  - "Last updated: YYYY-MM-DD" footer per page

## Files to Touch
- `web/src/pages/settings/knowledgebase.tsx` — MDX renderer (lazy import)
- `web/src/content/knowledge-base/*.mdx` (NEW — 9 files)
- `web/src/components/kb/*` — StepperCard, FlowDiagram, RequestResponsePopup, DecisionTree (NEW)
- Dependencies: `@mdx-js/react`, `mermaid` (diagrams)

## Risks & Regression
- **Risk 1 — Content volume:** 9 sections × rich content = large initial write. Mitigation: ship with bare-minimum content per section; expand iteratively in follow-up stories. Each section has "Coming soon" placeholder if content incomplete.
- **Risk 2 — MDX bundle size:** MDX adds ~50KB gzipped. Acceptable; lazy-load KB route chunk.
- **Risk 3 — Mermaid rendering cost:** Lazy init, only render when section open.
- **Risk 4 — Search performance:** Full-text search over 9 MDX files — lightweight (FuseJS client-side search).

## Test Plan
- Browser: navigate through 9 sections, TOC works, Cmd+K finds terms
- Each protocol popup opens + shows wire format
- PDF export generates readable output
- Print CSS works

## Plan Reference
Priority: P1 · Effort: L · Wave: 9 · Depends: FIX-216 (SlidePanel pattern for popups)
