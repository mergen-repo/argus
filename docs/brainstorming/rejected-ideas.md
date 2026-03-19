# Rejected Ideas — Argus

| # | Date | Idea | Reason |
|---|------|------|--------|
| R-001 | 2026-03-18 | FreeRADIUS as core AAA engine | No Diameter support, SQL bottleneck at 2K pps, no native policy engine, config complexity, not cloud-native. Custom Go implementation preferred for 10M+ scale. |
| R-002 | 2026-03-18 | Hybrid approach (FreeRADIUS + custom wrapper) | Two language worlds (C + Go), fragile integration points, FreeRADIUS config model constraining, Diameter still missing. Technical debt not worth the faster start. |
| R-003 | 2026-03-18 | Phased MVP (basic v1 → features in v2/v3) | Cannot compete with established global players with a partial product. Market entry requires full feature parity. |
| R-004 | 2026-03-18 | Own SM-DP+ server (eSIM Level C) | GSMA SAS-SM certification + FIPS 140-2 L3 HSM = hundreds of thousands $, months of audit, unrealistic for solo dev. BTK requires local operator anyway. |
| R-005 | 2026-03-18 | SGP.32 eIM in v1 | Ecosystem too immature (spec released 2023), limited device/chipset support. Deferred to future. |
| R-006 | 2026-03-18 | VoWiFi/WiFi Offload AAA | IoT/M2M focus, voice not in scope. Different market segment. |
| R-007 | 2026-03-18 | TACACS+ support | Network device admin protocol, not IoT SIM management. |
| R-008 | 2026-03-18 | Geo-fencing in v1 | Nice-to-have, can be added to policy engine later without architectural changes. |
| R-009 | 2026-03-18 | Device management (firmware/health) | Different product category. Argus manages SIM/connectivity, not the device itself. |

---
