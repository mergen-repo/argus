// Copy source: docs/GLOSSARY.md (single source of truth).
// When adding a term here, also add/update in GLOSSARY.md.
export const GLOSSARY_TOOLTIPS: Record<string, string> = {
  MCC: 'Mobile Country Code (3 digits identifying country, e.g. 286 = Turkey)',
  MNC: 'Mobile Network Code (2-3 digits identifying operator within country)',
  EID: 'eUICC Identifier (32-digit eSIM chip serial)',
  MSISDN: 'Mobile Station ISDN Number (phone number)',
  APN: 'Access Point Name (network entry identifier)',
  IMSI: 'International Mobile Subscriber Identity (15-digit subscriber ID)',
  ICCID: 'Integrated Circuit Card Identifier (SIM card serial)',
  CoA: 'Change of Authorization (mid-session policy update, RFC 5176)',
  SLA: 'Service Level Agreement (uptime contract)',
  static_ip: 'Static IP — an IP address permanently assigned to a specific SIM via pool reservation. Survives re-authentication and session teardown. Reclaim grace window configurable per pool.',
}
