# Glossary — Argus

## AAA & Protocol Terms

| Term | Definition | Context |
|------|-----------|---------|
| AAA | Authentication, Authorization, and Accounting | Core function of Argus L1 engine |
| RADIUS | Remote Authentication Dial-In User Service (RFC 2865/2866) | Primary AAA protocol, UDP-based |
| Diameter | Next-gen AAA protocol (RFC 6733) | Used for Gx (policy) and Gy (charging) with operators |
| RadSec | RADIUS over TLS (RFC 6614) | Encrypted RADIUS transport |
| Gx | Diameter interface for policy and charging control | Communicates QoS/policy rules to P-GW |
| Gy | Diameter interface for online charging | Real-time charging/quota management |
| CoA | Change of Authorization (RFC 5176) | Dynamically modify active sessions (e.g., apply new policy) |
| DM | Disconnect Message (RFC 5176) | Force-terminate an active session |
| EAP-SIM | Extensible Authentication Protocol for SIM | Authenticates using GSM SIM credentials |
| EAP-AKA | EAP Authentication and Key Agreement | Authenticates using USIM (3G/4G) credentials |
| EAP-AKA' | Enhanced EAP-AKA for 5G | Adds key separation for 5G networks |
| SBA | Service-Based Architecture | 5G core network architecture using HTTP/2 APIs |
| AUSF | Authentication Server Function | 5G core network function for authentication |
| UDM | Unified Data Management | 5G core network function for subscriber data |
| NAS | Network Access Server | Device that sends RADIUS/Diameter requests to Argus |
| PCRF | Policy and Charging Rules Function | LTE network element; Argus implements a mini-PCRF |
| PCEF | Policy and Charging Enforcement Function | Enforcement point in P-GW; receives rules from PCRF |

## SIM & Mobile Terms

| Term | Definition | Context |
|------|-----------|---------|
| SIM | Subscriber Identity Module | Physical or embedded chip for network authentication |
| eSIM | Embedded SIM (eUICC) | Programmable SIM, profiles downloadable over-the-air |
| eUICC | Embedded Universal Integrated Circuit Card | Hardware chip that hosts eSIM profiles |
| IMSI | International Mobile Subscriber Identity | Unique subscriber identifier (up to 15 digits) |
| MSISDN | Mobile Station International Subscriber Directory Number | Phone number associated with SIM |
| ICCID | Integrated Circuit Card Identifier | Unique SIM card serial number (up to 22 digits) |
| EID | eUICC Identifier | Unique identifier for eSIM hardware |
| IMEI | International Mobile Equipment Identity | Device hardware identifier |
| SM-DP+ | Subscription Manager - Data Preparation+ | Server that prepares and delivers eSIM profiles (SGP.22) |
| OTA | Over-The-Air | Remote SIM management via APDU commands |
| APDU | Application Protocol Data Unit | Commands sent to SIM card for configuration |
| MNO | Mobile Network Operator | Turkcell, Vodafone, TT Mobile |
| MVNO | Mobile Virtual Network Operator | Virtual operator using MNO infrastructure |
| SGP.02 | GSMA M2M eSIM specification | Legacy M2M remote provisioning (push model) |
| SGP.22 | GSMA Consumer eSIM specification | Consumer eSIM with SM-DP+ (pull model) |
| SGP.32 | GSMA IoT eSIM specification | New IoT-specific eSIM standard (2023) |

## Network Terms

| Term | Definition | Context |
|------|-----------|---------|
| APN | Access Point Name | Network entry point identifier; determines routing and services |
| Private APN | Operator-defined APN for enterprise traffic isolation | Core Argus management target |
| P-GW | Packet Data Network Gateway (4G) | Routes traffic based on APN, enforces policy |
| GGSN | Gateway GPRS Support Node (3G) | Legacy equivalent of P-GW |
| RAT | Radio Access Technology | NB-IoT, LTE-M, 4G LTE, 5G NR |
| NB-IoT | Narrowband IoT | Low-power, low-bandwidth cellular for IoT sensors |
| LTE-M | LTE for Machines (Cat-M1) | Medium-bandwidth cellular for IoT with mobility |
| SoR | Steering of Roaming | Directing SIM to preferred network when multiple available |
| Network Slice | Isolated virtual network within 5G | Dedicated resources for specific use case (e.g., IoT slice) |
| DNN | Data Network Name | 5G equivalent of APN |

## Argus Platform Terms

| Term | Definition | Context |
|------|-----------|---------|
| Tenant | An isolated enterprise customer account in Argus | Multi-tenant isolation unit |
| Operator Adapter | Pluggable module connecting Argus to a specific MNO | System-level, shared across tenants |
| Operator Grant | Permission for a tenant to use a specific operator adapter | Tenant ↔ Operator access control |
| Policy Version | Immutable snapshot of a policy rule set | Versioned for rollback/staged rollout |
| Policy DSL | Domain-specific language for defining policy rules | e.g., `IF usage > 1GB THEN throttle_to(256kbps)` |
| Staged Rollout | Gradual policy deployment: 1% → 10% → 100% of affected SIMs | Canary deployment for policies |
| Dry-Run | Policy simulation without applying changes | Shows impact before commit |
| SIM Segment | Saved filter/group of SIMs based on criteria | Group-first UX navigation unit |
| Circuit Breaker | Pattern that disables a failing operator after N consecutive errors | Prevents cascade failures |
| Dead Letter Queue | Storage for failed/unprocessable AAA requests | For retry and investigation |
| IP Reclaim | Process of returning terminated SIM's IP to pool after grace period | Configurable retention |
| Pseudonymization | Replacing personal identifiers with irreversible hashes | KVKK/GDPR purge compliance |
| Hash Chain | Sequential hashing linking each audit log entry to previous | Tamper detection |
| CDR | Call Detail Record | Usage record: bytes, duration, cost, RAT-type per session |

## Regulatory Terms

| Term | Definition | Context |
|------|-----------|---------|
| BTK | Bilgi Teknolojileri ve İletişim Kurumu | Turkish telecom regulator |
| KVKK | Kişisel Verilerin Korunması Kanunu | Turkish personal data protection law |
| GDPR | General Data Protection Regulation | EU data protection regulation |
| ISO 27001 | Information Security Management System standard | Audit logging and access control |
| GSMA SAS-SM | Security Accreditation Scheme for SM-DP+ | Certification required to host own SM-DP+ |
| FIPS 140-2 L3 | Federal Information Processing Standard, Level 3 | HSM security certification for SM-DP+ |

## Abbreviations

| Abbreviation | Full Form |
|-------------|-----------|
| HA | High Availability |
| IPAM | IP Address Management |
| FUP | Fair Usage Policy |
| QoS | Quality of Service |
| RBAC | Role-Based Access Control |
| JWT | JSON Web Token |
| TOTP | Time-based One-Time Password |
| SPA | Single Page Application |
| SSE | Server-Sent Events |
| NOC | Network Operations Center |
| HSM | Hardware Security Module |
| TPS | Transactions Per Second |
| CDN | Content Delivery Network |
| CIDR | Classless Inter-Domain Routing |
| CORS | Cross-Origin Resource Sharing |
| CSP | Content Security Policy |
| HMAC | Hash-based Message Authentication Code |
| SMPP | Short Message Peer-to-Peer (SMS protocol) |
| PgBouncer | PostgreSQL connection pooler |
| gRPC | Google Remote Procedure Call |
| CEL | Common Expression Language |
| DSL | Domain-Specific Language |
| BRAS | Broadband Remote Access Server |
| BNG | Broadband Network Gateway |
| N3IWF | Non-3GPP Interworking Function (5G) |
| ePDG | Evolved Packet Data Gateway |
