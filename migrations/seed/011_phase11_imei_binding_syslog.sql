-- SEED-11: Phase 11 — IMEI Pools, SIM-Device Binding, IMEI History,
-- Per-SIM Allowlist, Syslog Destinations, Phase 11 Audit Trail
--
-- Idempotent: ON CONFLICT DO NOTHING + deterministic ORDER BY id LIMIT/OFFSET
-- so re-runs touch identical rows.
--
-- Schema reality (per migration truth):
--   binding_mode CHECK: strict | allowlist | first-use | tac-lock | grace-period | soft
--   binding_status CHECK: verified | pending | mismatch | unbound | disabled
--   imei_history.capture_protocol CHECK: radius | diameter_s6a | 5g_sba
--   sim_imei_allowlist columns: sim_id, imei, added_at, added_by  (NO expires_at)
--   imei_blacklist.imported_from CHECK: manual | gsma_ceir | operator_eir
--
-- audit_logs hash chain: insert with placeholder zeros; `make db-seed`
-- runs `argus repair-audit` post-seed which rebuilds the chain.

BEGIN;

-- ============================================================
-- IMEI WHITELIST (Tenant 00000000-...0001 — argus admin home tenant)
-- 50+ entries: full_imei + tac_range mix
-- TACs use real device manufacturer 8-digit prefixes for IMEI Lookup demo.
-- ============================================================
INSERT INTO imei_whitelist (id, tenant_id, kind, imei_or_tac, device_model, description, created_by, created_at, updated_at) VALUES
-- TAC ranges (8-digit prefixes — realistic vendor TACs)
('70000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'tac_range', '35327309', 'Apple iPhone 13', 'Apple TAC for fleet handsets',                           '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '30 days', NOW() - INTERVAL '30 days'),
('70000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'tac_range', '35922510', 'Samsung Galaxy S22', 'Samsung TAC for corporate fleet',                    '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '30 days', NOW() - INTERVAL '30 days'),
('70000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'tac_range', '86730203', 'Quectel BG95', 'Quectel BG95 IoT module — approved fleet device',           '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '60 days', NOW() - INTERVAL '60 days'),
('70000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', 'tac_range', '35453308', 'Sierra Wireless EM7565', 'Sierra Wireless EM7565 LTE-A modem',              '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '60 days', NOW() - INTERVAL '60 days'),
('70000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001', 'tac_range', '86471004', 'Quectel EC25', 'Quectel EC25 LTE Cat-4 module',                              '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '45 days', NOW() - INTERVAL '45 days'),
('70000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000001', 'tac_range', '86891005', 'Quectel BG96', 'Quectel BG96 LTE-M / NB-IoT module',                         '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '45 days', NOW() - INTERVAL '45 days'),
('70000000-0000-0000-0000-000000000007', '00000000-0000-0000-0000-000000000001', 'tac_range', '35878006', 'u-blox SARA-R5', 'u-blox SARA-R5 5G NB-IoT/LTE-M module',                   '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '20 days', NOW() - INTERVAL '20 days'),
('70000000-0000-0000-0000-000000000008', '00000000-0000-0000-0000-000000000001', 'tac_range', '35714508', 'Telit ME910C1', 'Telit ME910C1 LTE-M module',                                '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '20 days', NOW() - INTERVAL '20 days'),
('70000000-0000-0000-0000-000000000009', '00000000-0000-0000-0000-000000000001', 'tac_range', '35327410', 'Apple iPhone 14', 'Apple iPhone 14 — corporate handsets',                    '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '15 days', NOW() - INTERVAL '15 days'),
('70000000-0000-0000-0000-000000000010', '00000000-0000-0000-0000-000000000001', 'tac_range', '35922611', 'Samsung Galaxy S23', 'Samsung Galaxy S23 corporate fleet',                   '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '15 days', NOW() - INTERVAL '15 days'),
-- full_imei entries (15-digit, valid Luhn-ish, demo-only)
('70000000-0000-0000-0000-000000000011', '00000000-0000-0000-0000-000000000001', 'full_imei', '353273090012345', 'Apple iPhone 13 - Driver Tablet', 'Pinned to fleet tablet TR-001',  '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '40 days', NOW() - INTERVAL '40 days'),
('70000000-0000-0000-0000-000000000012', '00000000-0000-0000-0000-000000000001', 'full_imei', '359225100023456', 'Samsung S22 - Field Engineer 12', 'Locked to engineer Ahmet Yılmaz', '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '40 days', NOW() - INTERVAL '40 days'),
('70000000-0000-0000-0000-000000000013', '00000000-0000-0000-0000-000000000001', 'full_imei', '867302030034567', 'Quectel BG95 — Asset Tracker AT-103', 'Vehicle tracker AT-103',     '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '35 days', NOW() - INTERVAL '35 days'),
('70000000-0000-0000-0000-000000000014', '00000000-0000-0000-0000-000000000001', 'full_imei', '354533080045678', 'Sierra Wireless EM7565 — Cargo van CV-22', 'Cargo van CV-22 router', '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '35 days', NOW() - INTERVAL '35 days'),
('70000000-0000-0000-0000-000000000015', '00000000-0000-0000-0000-000000000001', 'full_imei', '864710040056789', 'Quectel EC25 — Smart Meter SM-501', 'Smart meter İstanbul/Beşiktaş', '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '25 days', NOW() - INTERVAL '25 days'),
('70000000-0000-0000-0000-000000000016', '00000000-0000-0000-0000-000000000001', 'full_imei', '868910050067890', 'Quectel BG96 — POS Terminal POS-021', 'POS terminal Ankara/Çankaya', '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '25 days', NOW() - INTERVAL '25 days'),
('70000000-0000-0000-0000-000000000017', '00000000-0000-0000-0000-000000000001', 'full_imei', '358780060078901', 'u-blox SARA-R5 — Streetlight SL-307', 'Streetlight Bursa/Nilüfer',  '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days'),
('70000000-0000-0000-0000-000000000018', '00000000-0000-0000-0000-000000000001', 'full_imei', '357145080089012', 'Telit ME910C1 — Water Meter WM-118', 'Water meter İzmir/Karşıyaka',  '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days'),
('70000000-0000-0000-0000-000000000019', '00000000-0000-0000-0000-000000000001', 'full_imei', '353274100090123', 'Apple iPhone 14 — Manager Demo M5', 'Manager Elif Demir',           '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '5 days',  NOW() - INTERVAL '5 days'),
('70000000-0000-0000-0000-000000000020', '00000000-0000-0000-0000-000000000001', 'full_imei', '359226110001234', 'Samsung S23 — Sales Rep SR-7',          'Sales rep Mehmet Kaya',  '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '5 days',  NOW() - INTERVAL '5 days')
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

-- Generated bulk additions (tenant 1: 30 more full_imei rows for pagination)
INSERT INTO imei_whitelist (tenant_id, kind, imei_or_tac, device_model, description, created_by, created_at, updated_at)
SELECT
  '00000000-0000-0000-0000-000000000001'::uuid,
  'full_imei',
  '8673020300' || LPAD((100 + g)::text, 5, '0'),
  'Quectel BG95 — Asset Tracker AT-' || LPAD((100 + g)::text, 3, '0'),
  'Bulk-imported asset tracker fleet entry #' || g,
  '00000000-0000-0000-0000-000000000010'::uuid,
  NOW() - (g || ' hours')::interval,
  NOW() - (g || ' hours')::interval
FROM generate_series(1, 30) AS g
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

-- Tenant 2 (Nar Teknoloji) — 8 whitelist entries for multi-tenant smoke
INSERT INTO imei_whitelist (id, tenant_id, kind, imei_or_tac, device_model, description, created_at, updated_at) VALUES
('70000000-0000-0000-0000-000000000101', '10000000-0000-0000-0000-000000000001', 'tac_range', '35327309', 'Apple iPhone 13',          'Nar Teknoloji approved Apple TAC',          NOW() - INTERVAL '20 days', NOW() - INTERVAL '20 days'),
('70000000-0000-0000-0000-000000000102', '10000000-0000-0000-0000-000000000001', 'tac_range', '35922510', 'Samsung S22',              'Nar Teknoloji corporate Samsung',           NOW() - INTERVAL '20 days', NOW() - INTERVAL '20 days'),
('70000000-0000-0000-0000-000000000103', '10000000-0000-0000-0000-000000000001', 'tac_range', '86730203', 'Quectel BG95',             'Nar Teknoloji IoT fleet module',            NOW() - INTERVAL '15 days', NOW() - INTERVAL '15 days'),
('70000000-0000-0000-0000-000000000104', '10000000-0000-0000-0000-000000000001', 'full_imei', '353273090099001', 'iPhone 13 — Director Tablet', 'CEO Yusuf Demir',                NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days'),
('70000000-0000-0000-0000-000000000105', '10000000-0000-0000-0000-000000000001', 'full_imei', '359225100099002', 'Samsung S22 — VP Sales',       'VP Zeynep Aslan',                NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days'),
('70000000-0000-0000-0000-000000000106', '10000000-0000-0000-0000-000000000001', 'full_imei', '867302030099003', 'Quectel BG95 — Tracker T-901', 'Fleet vehicle tracker T-901',    NOW() - INTERVAL '5 days',  NOW() - INTERVAL '5 days'),
('70000000-0000-0000-0000-000000000107', '10000000-0000-0000-0000-000000000001', 'full_imei', '354533080099004', 'Sierra EM7565 — Cargo C-12',   'Cargo van C-12 router',          NOW() - INTERVAL '5 days',  NOW() - INTERVAL '5 days'),
('70000000-0000-0000-0000-000000000108', '10000000-0000-0000-0000-000000000001', 'full_imei', '358780060099005', 'u-blox SARA-R5 — Light L-50',  'Streetlight Sarıyer L-50',       NOW() - INTERVAL '2 days',  NOW() - INTERVAL '2 days')
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

-- ============================================================
-- IMEI GREYLIST (50 entries — quarantine)
-- ============================================================
INSERT INTO imei_greylist (id, tenant_id, kind, imei_or_tac, device_model, description, quarantine_reason, created_by, created_at, updated_at) VALUES
('71000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'tac_range', '35876405', 'Unknown Chinese OEM',           'TAC under review by procurement',                'Awaiting GSMA TAC verification',                                  '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '14 days', NOW() - INTERVAL '14 days'),
('71000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'tac_range', '35712502', 'Refurbished iPhone batch',      'Batch from secondary market',                    'Refurbished — needs IMEI re-validation',                          '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '14 days', NOW() - INTERVAL '14 days'),
('71000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'full_imei', '358764050012345', 'Unknown handset',          'Reported by AAA on 2026-04-25',                  'Single observation; investigating fraud risk',                    '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days'),
('71000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', 'full_imei', '357125020023456', 'Refurbished iPhone',       'Customer-reported swap',                          'Pending field engineer site visit',                                '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '8 days',  NOW() - INTERVAL '8 days'),
('71000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001', 'full_imei', '352901660034567', 'Unidentified Android',     'Roaming partner observation',                     'Awaiting partner operator confirmation',                          '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '6 days',  NOW() - INTERVAL '6 days'),
('71000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000001', 'full_imei', '353278920045678', 'Cloned IMEI suspected',    'Two simultaneous attaches detected',              'Suspected clone — pending forensic analysis',                     '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '4 days',  NOW() - INTERVAL '4 days'),
('71000000-0000-0000-0000-000000000007', '00000000-0000-0000-0000-000000000001', 'full_imei', '866612340056789', 'Unknown IoT module',       'IMEI not in vendor catalog',                      'Vendor catalog missing — TAC lookup inconclusive',                '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '3 days',  NOW() - INTERVAL '3 days'),
('71000000-0000-0000-0000-000000000008', '00000000-0000-0000-0000-000000000001', 'full_imei', '358122340067890', 'Aging fleet device',       'Customer flagged for replacement',                'Hardware EOL — replacement scheduled Q2 2026',                    '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '2 days',  NOW() - INTERVAL '2 days'),
('71000000-0000-0000-0000-000000000009', '00000000-0000-0000-0000-000000000001', 'full_imei', '359334450078901', 'Stolen device alert',      'IMEI matches operator stolen list',               'Cross-checked with Türk Telekom stolen list — pending block',     '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '1 days',  NOW() - INTERVAL '1 days'),
('71000000-0000-0000-0000-000000000010', '00000000-0000-0000-0000-000000000001', 'full_imei', '352778880089012', 'Possible IMEI tumbling',   'Multiple SIMs observed swapping',                 'IMEI tumbling pattern — investigating',                           '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '12 hours', NOW() - INTERVAL '12 hours')
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

INSERT INTO imei_greylist (tenant_id, kind, imei_or_tac, device_model, description, quarantine_reason, created_by, created_at, updated_at)
SELECT
  '00000000-0000-0000-0000-000000000001'::uuid,
  'full_imei',
  '3534567' || LPAD((100 + g)::text, 8, '0'),
  'Suspected refurbished device #' || g,
  'Auto-flagged by anomaly detector',
  CASE (g % 4) WHEN 0 THEN 'Frequent IMEI swap detected'
               WHEN 1 THEN 'Awaiting field investigation'
               WHEN 2 THEN 'Under fraud risk review'
               ELSE 'Pending TAC re-verification' END,
  '00000000-0000-0000-0000-000000000010'::uuid,
  NOW() - (g || ' hours')::interval,
  NOW() - (g || ' hours')::interval
FROM generate_series(1, 40) AS g
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

-- Tenant 2 (Nar) — 6 greylist entries
INSERT INTO imei_greylist (tenant_id, kind, imei_or_tac, device_model, description, quarantine_reason, created_at, updated_at) VALUES
('10000000-0000-0000-0000-000000000001', 'full_imei', '358764050099101', 'Suspected swap', 'Customer Ali Veli reports lost handset',  'Awaiting customer confirmation',     NOW() - INTERVAL '5 days', NOW() - INTERVAL '5 days'),
('10000000-0000-0000-0000-000000000001', 'full_imei', '357125020099102', 'Refurbished',     'Bought via secondary market',             'Pending IMEI re-validation',         NOW() - INTERVAL '4 days', NOW() - INTERVAL '4 days'),
('10000000-0000-0000-0000-000000000001', 'full_imei', '352901660099103', 'Unknown OEM',     'Imported batch from Asia',                'Awaiting GSMA verification',         NOW() - INTERVAL '3 days', NOW() - INTERVAL '3 days'),
('10000000-0000-0000-0000-000000000001', 'full_imei', '353278920099104', 'Possible clone',  'Two attaches at 13:04 + 13:07 same day',  'Forensic team review',                NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days'),
('10000000-0000-0000-0000-000000000001', 'tac_range', '35876405',        'Unknown TAC',     'TAC not in fleet catalog',                'Procurement to verify with vendor',   NOW() - INTERVAL '7 days', NOW() - INTERVAL '7 days'),
('10000000-0000-0000-0000-000000000001', 'tac_range', '35712502',        'Refurbished batch', 'Q1 secondary-market purchase',           'Awaiting batch verification',         NOW() - INTERVAL '6 days', NOW() - INTERVAL '6 days')
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

-- ============================================================
-- IMEI BLACKLIST (50 entries — block reasons + sources)
-- ============================================================
INSERT INTO imei_blacklist (id, tenant_id, kind, imei_or_tac, device_model, description, block_reason, imported_from, created_by, created_at, updated_at) VALUES
('72000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'full_imei', '353273099911111', 'Stolen iPhone 13',         'Reported lost on 2026-03-15',                  'Stolen — police report TR-2026-3471',          'gsma_ceir',     '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '50 days', NOW() - INTERVAL '50 days'),
('72000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'full_imei', '359225100022222', 'Stolen Samsung S22',       'Reported by customer Ahmet Yılmaz',            'Stolen — police report TR-2026-3892',          'manual',        '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '40 days', NOW() - INTERVAL '40 days'),
('72000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'full_imei', '867302030033333', 'Cloned tracker IMEI',      'Forensic confirmed clone',                      'Cloned IMEI — multi-attach evidence',           'operator_eir',  '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '35 days', NOW() - INTERVAL '35 days'),
('72000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', 'full_imei', '354533080044444', 'Fraud — chargeback IMEI',  'Customer disputed charges',                     'Fraud chargeback — IMEI permanently blocked',    'manual',        '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '30 days', NOW() - INTERVAL '30 days'),
('72000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001', 'full_imei', '864710040055555', 'GSMA-blocked IMEI',        'Imported from GSMA CEIR feed',                  'GSMA CEIR blocked — international stolen',      'gsma_ceir',     '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '25 days', NOW() - INTERVAL '25 days'),
('72000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000001', 'full_imei', '868910050066666', 'Operator EIR block',       'Pushed by Türk Telekom EIR',                    'Operator EIR — Türk Telekom enforcement',       'operator_eir',  '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '20 days', NOW() - INTERVAL '20 days'),
('72000000-0000-0000-0000-000000000007', '00000000-0000-0000-0000-000000000001', 'full_imei', '358780060077777', 'Stolen streetlight tracker', 'Reported by İBB asset team',                  'Stolen — municipal report İBB-2026-211',        'manual',        '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '15 days', NOW() - INTERVAL '15 days'),
('72000000-0000-0000-0000-000000000008', '00000000-0000-0000-0000-000000000001', 'full_imei', '357145080088888', 'Stolen water meter',        'Reported by ASKİ asset team',                  'Stolen — utility report ASKİ-2026-088',          'manual',        '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days'),
('72000000-0000-0000-0000-000000000009', '00000000-0000-0000-0000-000000000001', 'full_imei', '353274100099999', 'Stolen iPhone 14',          'Reported by manager Elif Demir',               'Stolen — police report TR-2026-4912',           'gsma_ceir',     '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '7 days',  NOW() - INTERVAL '7 days'),
('72000000-0000-0000-0000-000000000010', '00000000-0000-0000-0000-000000000001', 'full_imei', '359226110011010', 'Cloned VP handset',         'Two simultaneous attaches detected',            'Cloned IMEI — operator EIR confirmed',           'operator_eir',  '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '3 days',  NOW() - INTERVAL '3 days')
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

INSERT INTO imei_blacklist (tenant_id, kind, imei_or_tac, device_model, description, block_reason, imported_from, created_by, created_at, updated_at)
SELECT
  '00000000-0000-0000-0000-000000000001'::uuid,
  'full_imei',
  '3596666' || LPAD((900 + g)::text, 8, '0'),
  'GSMA CEIR feed entry #' || g,
  'Imported from GSMA daily feed',
  CASE (g % 3) WHEN 0 THEN 'GSMA CEIR — international stolen device'
               WHEN 1 THEN 'GSMA CEIR — fraud-related block'
               ELSE 'GSMA CEIR — partner operator escalation' END,
  'gsma_ceir',
  '00000000-0000-0000-0000-000000000010'::uuid,
  NOW() - (g || ' hours')::interval,
  NOW() - (g || ' hours')::interval
FROM generate_series(1, 40) AS g
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

-- Tenant 2 (Nar) — 6 blacklist entries
INSERT INTO imei_blacklist (tenant_id, kind, imei_or_tac, device_model, description, block_reason, imported_from, created_at, updated_at) VALUES
('10000000-0000-0000-0000-000000000001', 'full_imei', '353273090099201', 'Stolen iPhone',        'CEO handset stolen 2026-04-10',           'Stolen — police report TR-2026-9921',         'manual',         NOW() - INTERVAL '25 days', NOW() - INTERVAL '25 days'),
('10000000-0000-0000-0000-000000000001', 'full_imei', '359225100099202', 'Stolen Samsung',       'VP handset stolen 2026-04-15',            'Stolen — police report TR-2026-9925',         'gsma_ceir',      NOW() - INTERVAL '20 days', NOW() - INTERVAL '20 days'),
('10000000-0000-0000-0000-000000000001', 'full_imei', '867302030099203', 'Cloned tracker',       'Tracker T-901 cloned IMEI detected',      'Cloned — forensic team confirmed',             'operator_eir',   NOW() - INTERVAL '15 days', NOW() - INTERVAL '15 days'),
('10000000-0000-0000-0000-000000000001', 'full_imei', '354533080099204', 'Fraud chargeback',     'Customer chargeback dispute resolved',     'Fraud chargeback — permanently blocked',       'manual',         NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days'),
('10000000-0000-0000-0000-000000000001', 'tac_range', '35987104',         'Suspect Asian batch',  'Whole TAC blocked — fraud risk',          'TAC-wide block — operator decision',           'manual',         NOW() - INTERVAL '8 days',  NOW() - INTERVAL '8 days'),
('10000000-0000-0000-0000-000000000001', 'tac_range', '86412303',         'GSMA-flagged TAC',     'GSMA flagged TAC for fraud',              'GSMA CEIR — TAC-wide block',                   'gsma_ceir',      NOW() - INTERVAL '5 days',  NOW() - INTERVAL '5 days')
ON CONFLICT (tenant_id, imei_or_tac) DO NOTHING;

COMMIT;

-- ============================================================
-- SIM-DEVICE BINDING UPDATES (deterministic distribution)
-- Tenant 00000000-...0001 has 163 SIMs. Strategy:
--   - 50 SIMs (offset 0..49)   → binding_mode='strict',       binding_status='verified',  bound_imei set
--   - 20 SIMs (offset 50..69)  → binding_mode='allowlist',    binding_status='verified',  bound_imei set
--   - 20 SIMs (offset 70..89)  → binding_mode='tac-lock',     binding_status='verified',  bound_imei set
--   - 10 SIMs (offset 90..99)  → binding_mode='strict',       binding_status='mismatch',  bound_imei set
--   - 10 SIMs (offset 100..109)→ binding_mode='grace-period', binding_status='pending',   bound_imei set, grace_expires_at set
--   - 5  SIMs (offset 110..114)→ binding_mode='soft',         binding_status='unbound',   bound_imei NULL (capture-only)
--   - rest → binding_mode NULL (legacy capture-only — already current state)
-- ============================================================

-- Strict + verified: 50 SIMs
WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  bound_imei = '353273090' || LPAD((100000 + r.rn)::text, 6, '0'),
  binding_mode = 'strict',
  binding_status = 'verified',
  binding_verified_at = NOW() - INTERVAL '7 days',
  last_imei_seen_at = NOW() - INTERVAL '2 hours'
FROM ranked r
WHERE s.id = r.id AND r.rn < 50;

-- Allowlist + verified: 20 SIMs (offset 50..69)
WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  bound_imei = '359225100' || LPAD((200000 + r.rn)::text, 6, '0'),
  binding_mode = 'allowlist',
  binding_status = 'verified',
  binding_verified_at = NOW() - INTERVAL '14 days',
  last_imei_seen_at = NOW() - INTERVAL '4 hours'
FROM ranked r
WHERE s.id = r.id AND r.rn >= 50 AND r.rn < 70;

-- TAC-lock + verified: 20 SIMs (offset 70..89)
WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  bound_imei = '867302030' || LPAD((300000 + r.rn)::text, 6, '0'),
  binding_mode = 'tac-lock',
  binding_status = 'verified',
  binding_verified_at = NOW() - INTERVAL '21 days',
  last_imei_seen_at = NOW() - INTERVAL '6 hours'
FROM ranked r
WHERE s.id = r.id AND r.rn >= 70 AND r.rn < 90;

-- Strict + mismatch: 10 SIMs (offset 90..99) — drives SCR-021 mismatch panel
WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  bound_imei = '354533080' || LPAD((400000 + r.rn)::text, 6, '0'),
  binding_mode = 'strict',
  binding_status = 'mismatch',
  binding_verified_at = NOW() - INTERVAL '30 days',
  last_imei_seen_at = NOW() - INTERVAL '15 minutes'
FROM ranked r
WHERE s.id = r.id AND r.rn >= 90 AND r.rn < 100;

-- Grace-period + pending: 10 SIMs (offset 100..109) — drives SCR-021f re-pair workflow
WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  bound_imei = '358780060' || LPAD((500000 + r.rn)::text, 6, '0'),
  binding_mode = 'grace-period',
  binding_status = 'pending',
  binding_verified_at = NOW() - INTERVAL '60 days',
  last_imei_seen_at = NOW() - INTERVAL '1 hour',
  binding_grace_expires_at = NOW() + INTERVAL '3 days' + (r.rn || ' hours')::interval
FROM ranked r
WHERE s.id = r.id AND r.rn >= 100 AND r.rn < 110;

-- Soft (capture-only) + unbound: 5 SIMs (offset 110..114)
WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  binding_mode = 'soft',
  binding_status = 'unbound',
  last_imei_seen_at = NOW() - INTERVAL '12 hours'
FROM ranked r
WHERE s.id = r.id AND r.rn >= 110 AND r.rn < 115;

-- ============================================================
-- SIM-IMEI ALLOWLIST (per-SIM whitelist for binding_mode='allowlist')
-- 2-5 IMEIs per SIM with binding_mode='allowlist'
-- ============================================================
WITH allowlist_sims AS (
  SELECT id FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
    AND binding_mode = 'allowlist'
)
INSERT INTO sim_imei_allowlist (sim_id, imei, added_at, added_by)
SELECT
  s.id,
  '359225100' || LPAD((200000 + ROW_NUMBER() OVER (ORDER BY s.id))::text, 6, '0'),
  NOW() - INTERVAL '14 days',
  '00000000-0000-0000-0000-000000000010'::uuid
FROM allowlist_sims s
ON CONFLICT (sim_id, imei) DO NOTHING;

-- Add 2nd IMEI per allowlist SIM (spare device)
WITH allowlist_sims AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) AS rn FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
    AND binding_mode = 'allowlist'
)
INSERT INTO sim_imei_allowlist (sim_id, imei, added_at, added_by)
SELECT
  s.id,
  '353273090' || LPAD((900000 + s.rn)::text, 6, '0'),
  NOW() - INTERVAL '7 days',
  '00000000-0000-0000-0000-000000000010'::uuid
FROM allowlist_sims s
ON CONFLICT (sim_id, imei) DO NOTHING;

-- Add 3rd IMEI for half of them (loaner device)
WITH allowlist_sims AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) AS rn FROM sims
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
    AND binding_mode = 'allowlist'
)
INSERT INTO sim_imei_allowlist (sim_id, imei, added_at, added_by)
SELECT
  s.id,
  '867302030' || LPAD((800000 + s.rn)::text, 6, '0'),
  NOW() - INTERVAL '3 days',
  '00000000-0000-0000-0000-000000000010'::uuid
FROM allowlist_sims s
WHERE s.rn % 2 = 0
ON CONFLICT (sim_id, imei) DO NOTHING;

-- ============================================================
-- IMEI HISTORY (5+ rows per SIM with bound_imei)
-- Time-distributed last 30 days; mix of capture protocols
-- Idempotency: skip entire block if any imei_history row exists for tenant 1
-- (because there's no natural unique key — re-runs would duplicate).
-- ============================================================
DO $$
BEGIN
IF NOT EXISTS (
  SELECT 1 FROM imei_history
  WHERE tenant_id = '00000000-0000-0000-0000-000000000001'::uuid
  LIMIT 1
) THEN

-- Verified SIMs: 5 history rows of matching IMEI (was_mismatch=FALSE)
WITH bound_sims AS (
  SELECT s.id, s.tenant_id, s.bound_imei, ROW_NUMBER() OVER (ORDER BY s.id) AS rn
  FROM sims s
  WHERE s.tenant_id = '00000000-0000-0000-0000-000000000001'
    AND s.bound_imei IS NOT NULL
    AND s.binding_status = 'verified'
)
INSERT INTO imei_history (tenant_id, sim_id, observed_imei, observed_software_version, observed_at, capture_protocol, nas_ip_address, was_mismatch, alarm_raised)
SELECT
  s.tenant_id,
  s.id,
  s.bound_imei,
  CASE (s.rn + g) % 4 WHEN 0 THEN '01' WHEN 1 THEN '02' WHEN 2 THEN '03' ELSE '04' END,
  NOW() - ((g * 6) || ' days')::interval,
  CASE (s.rn + g) % 3 WHEN 0 THEN 'radius' WHEN 1 THEN 'diameter_s6a' ELSE '5g_sba' END,
  ('10.0.' || ((s.rn % 250) + 1) || '.10')::inet,
  FALSE,
  FALSE
FROM bound_sims s
CROSS JOIN generate_series(0, 4) AS g;

-- Mismatch SIMs: 5 history rows where last 2 rows show DIFFERENT IMEI + was_mismatch=TRUE
WITH bound_sims AS (
  SELECT s.id, s.tenant_id, s.bound_imei, ROW_NUMBER() OVER (ORDER BY s.id) AS rn
  FROM sims s
  WHERE s.tenant_id = '00000000-0000-0000-0000-000000000001'
    AND s.bound_imei IS NOT NULL
    AND s.binding_status = 'mismatch'
)
INSERT INTO imei_history (tenant_id, sim_id, observed_imei, observed_software_version, observed_at, capture_protocol, nas_ip_address, was_mismatch, alarm_raised)
SELECT
  s.tenant_id,
  s.id,
  -- First 3 observations = bound IMEI (history); last 2 observations = different IMEI (current mismatch)
  CASE WHEN g < 3 THEN s.bound_imei
       ELSE '352778880' || LPAD((900000 + s.rn * 10 + g)::text, 6, '0')
  END,
  '01',
  NOW() - ((g * 5) || ' days')::interval,
  CASE g % 3 WHEN 0 THEN 'radius' WHEN 1 THEN 'diameter_s6a' ELSE '5g_sba' END,
  ('10.0.' || ((s.rn % 250) + 1) || '.20')::inet,
  CASE WHEN g >= 3 THEN TRUE ELSE FALSE END,
  CASE WHEN g >= 3 THEN TRUE ELSE FALSE END
FROM bound_sims s
CROSS JOIN generate_series(0, 4) AS g;

-- Pending grace SIMs: 4 history rows (mix verified + recent mismatch entering grace)
WITH bound_sims AS (
  SELECT s.id, s.tenant_id, s.bound_imei, ROW_NUMBER() OVER (ORDER BY s.id) AS rn
  FROM sims s
  WHERE s.tenant_id = '00000000-0000-0000-0000-000000000001'
    AND s.bound_imei IS NOT NULL
    AND s.binding_status = 'pending'
)
INSERT INTO imei_history (tenant_id, sim_id, observed_imei, observed_software_version, observed_at, capture_protocol, nas_ip_address, was_mismatch, alarm_raised)
SELECT
  s.tenant_id,
  s.id,
  CASE WHEN g = 3 THEN '358780060' || LPAD((700000 + s.rn)::text, 6, '0')
       ELSE s.bound_imei
  END,
  '02',
  NOW() - ((g * 8) || ' days')::interval,
  'radius',
  ('10.0.' || ((s.rn % 250) + 1) || '.30')::inet,
  CASE WHEN g = 3 THEN TRUE ELSE FALSE END,
  FALSE
FROM bound_sims s
CROSS JOIN generate_series(0, 3) AS g;

END IF;
END $$;

-- ============================================================
-- TENANT 2 (Nar Teknoloji) — token bindings + history
-- ============================================================
WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '10000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  bound_imei = '353273090' || LPAD((600000 + r.rn)::text, 6, '0'),
  binding_mode = 'strict',
  binding_status = 'verified',
  binding_verified_at = NOW() - INTERVAL '10 days',
  last_imei_seen_at = NOW() - INTERVAL '3 hours'
FROM ranked r
WHERE s.id = r.id AND r.rn < 20;

WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '10000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  bound_imei = '359225100' || LPAD((700000 + r.rn)::text, 6, '0'),
  binding_mode = 'allowlist',
  binding_status = 'verified',
  binding_verified_at = NOW() - INTERVAL '15 days',
  last_imei_seen_at = NOW() - INTERVAL '5 hours'
FROM ranked r
WHERE s.id = r.id AND r.rn >= 20 AND r.rn < 30;

WITH ranked AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) - 1 AS rn
  FROM sims
  WHERE tenant_id = '10000000-0000-0000-0000-000000000001'
)
UPDATE sims s SET
  bound_imei = '354533080' || LPAD((800000 + r.rn)::text, 6, '0'),
  binding_mode = 'strict',
  binding_status = 'mismatch',
  binding_verified_at = NOW() - INTERVAL '40 days',
  last_imei_seen_at = NOW() - INTERVAL '20 minutes'
FROM ranked r
WHERE s.id = r.id AND r.rn >= 30 AND r.rn < 33;

-- Tenant 2 IMEI history (idempotent guard)
DO $$
BEGIN
IF NOT EXISTS (
  SELECT 1 FROM imei_history
  WHERE tenant_id = '10000000-0000-0000-0000-000000000001'::uuid
  LIMIT 1
) THEN
  INSERT INTO imei_history (tenant_id, sim_id, observed_imei, observed_software_version, observed_at, capture_protocol, nas_ip_address, was_mismatch, alarm_raised)
  SELECT
    s.tenant_id, s.id, s.bound_imei, '01',
    NOW() - ((g * 5) || ' days')::interval,
    CASE g % 3 WHEN 0 THEN 'radius' WHEN 1 THEN 'diameter_s6a' ELSE '5g_sba' END,
    ('10.20.' || ((s.rn % 250) + 1) || '.10')::inet,
    FALSE, FALSE
  FROM (
    SELECT s.id, s.tenant_id, s.bound_imei, s.binding_status, ROW_NUMBER() OVER (ORDER BY s.id) AS rn
    FROM sims s
    WHERE s.tenant_id = '10000000-0000-0000-0000-000000000001'
      AND s.bound_imei IS NOT NULL
  ) s
  CROSS JOIN generate_series(0, 4) AS g
  WHERE s.binding_status = 'verified';
END IF;
END $$;

-- Tenant 2 sim allowlist
WITH allowlist_sims AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) AS rn FROM sims
  WHERE tenant_id = '10000000-0000-0000-0000-000000000001'
    AND binding_mode = 'allowlist'
)
INSERT INTO sim_imei_allowlist (sim_id, imei, added_at)
SELECT id, '359225100' || LPAD((700000 + rn)::text, 6, '0'), NOW() - INTERVAL '15 days' FROM allowlist_sims
ON CONFLICT (sim_id, imei) DO NOTHING;

WITH allowlist_sims AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) AS rn FROM sims
  WHERE tenant_id = '10000000-0000-0000-0000-000000000001'
    AND binding_mode = 'allowlist'
)
INSERT INTO sim_imei_allowlist (sim_id, imei, added_at)
SELECT id, '353273090' || LPAD((950000 + rn)::text, 6, '0'), NOW() - INTERVAL '8 days' FROM allowlist_sims
ON CONFLICT (sim_id, imei) DO NOTHING;

-- ============================================================
-- SYSLOG DESTINATIONS (4-6 demo destinations)
-- Mix of UDP/TCP/TLS, RFC 3164/5424, enabled/disabled, last_delivery_at
-- ============================================================
INSERT INTO syslog_destinations (id, tenant_id, name, host, port, transport, format, facility, severity_floor, filter_categories, filter_min_severity, tls_ca_pem, tls_client_cert_pem, tls_client_key_pem, enabled, last_delivery_at, last_error, created_by, created_at, updated_at) VALUES
('73000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001',
 'Datadog UDP (RFC 3164)', 'logs.datadog-tr.local', 514, 'udp', 'rfc3164', 16, 6,
 ARRAY['auth','policy','audit']::text[], 4,
 NULL, NULL, NULL,
 TRUE, NOW() - INTERVAL '2 minutes', NULL,
 '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '30 days', NOW() - INTERVAL '2 minutes'),

('73000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001',
 'Splunk TCP (RFC 5424)', 'splunk.argus-corp.local', 6514, 'tcp', 'rfc5424', 17, 5,
 ARRAY['auth','policy','audit','session','imei','system']::text[], 5,
 NULL, NULL, NULL,
 TRUE, NOW() - INTERVAL '15 seconds', NULL,
 '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '20 days', NOW() - INTERVAL '15 seconds'),

('73000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001',
 'SIEM TLS (RFC 5424)', 'siem.argus-soc.local', 6514, 'tls', 'rfc5424', 18, 4,
 ARRAY['audit','imei']::text[], 3,
 '-----BEGIN CERTIFICATE-----' || E'\n' ||
 'MIIDazCCAlOgAwIBAgIUDEMOCAFAKEROOTCAFORTESTING1234567890abcdefABw' || E'\n' ||
 '... (mock CA cert — DEV/SEED ONLY) ...' || E'\n' ||
 '-----END CERTIFICATE-----',
 '-----BEGIN CERTIFICATE-----' || E'\n' ||
 'MIIDazCCAlOgAwIBAgIUDEMOCLIENTCERT1234567890abcdefghijklmnopqrsAw' || E'\n' ||
 '... (mock client cert — DEV/SEED ONLY) ...' || E'\n' ||
 '-----END CERTIFICATE-----',
 '-----BEGIN PRIVATE KEY-----' || E'\n' ||
 'MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDEMOCLIENTKEYAw' || E'\n' ||
 '... (mock client key — DEV/SEED ONLY) ...' || E'\n' ||
 '-----END PRIVATE KEY-----',
 TRUE, NOW() - INTERVAL '5 minutes', NULL,
 '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '15 days', NOW() - INTERVAL '5 minutes'),

('73000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001',
 'Backup syslog (DISABLED)', 'backup-syslog.argus-dr.local', 514, 'udp', 'rfc3164', 19, 7,
 ARRAY[]::text[], NULL,
 NULL, NULL, NULL,
 FALSE, NULL, 'connection refused (probe failed 2026-04-25)',
 '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '60 days', NOW() - INTERVAL '10 days'),

('73000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001',
 'Local rsyslog (UDP debug)', 'rsyslog.argus-internal.local', 1514, 'udp', 'rfc5424', 20, 7,
 ARRAY['auth']::text[], 6,
 NULL, NULL, NULL,
 TRUE, NOW() - INTERVAL '1 minute', NULL,
 '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '7 days', NOW() - INTERVAL '1 minute'),

('73000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000001',
 'Compliance archive (TCP)', 'compliance-archive.argus-corp.local', 6514, 'tcp', 'rfc5424', 21, 3,
 ARRAY['audit','imei','policy']::text[], 2,
 NULL, NULL, NULL,
 TRUE, NOW() - INTERVAL '8 hours', 'TLS handshake timeout (transient — retried OK)',
 '00000000-0000-0000-0000-000000000010', NOW() - INTERVAL '5 days', NOW() - INTERVAL '8 hours'),

-- Tenant 2 (Nar) — 2 destinations
('73000000-0000-0000-0000-000000000101', '10000000-0000-0000-0000-000000000001',
 'Nar SIEM TCP', 'siem.nar.local', 6514, 'tcp', 'rfc5424', 16, 5,
 ARRAY['auth','audit']::text[], 4,
 NULL, NULL, NULL,
 TRUE, NOW() - INTERVAL '3 minutes', NULL,
 NULL, NOW() - INTERVAL '10 days', NOW() - INTERVAL '3 minutes'),

('73000000-0000-0000-0000-000000000102', '10000000-0000-0000-0000-000000000001',
 'Nar SOC TLS (RFC 5424)', 'soc.nar.local', 6514, 'tls', 'rfc5424', 17, 4,
 ARRAY['audit','imei','system']::text[], 3,
 '-----BEGIN CERTIFICATE-----' || E'\n... (mock) ...' || E'\n-----END CERTIFICATE-----',
 NULL, NULL,
 TRUE, NOW() - INTERVAL '10 minutes', NULL,
 NULL, NOW() - INTERVAL '7 days', NOW() - INTERVAL '10 minutes')
ON CONFLICT (tenant_id, name) DO NOTHING;

-- ============================================================
-- AUDIT LOGS — Phase 11 actions (200+ rows, time-distributed last 7 days)
-- Hash chain: trigger trg_audit_chain_guard rejects placeholder prev_hash.
-- We use session_replication_role=replica to bypass user triggers during
-- bulk seed insert; post-seed `argus repair-audit` rebuilds the chain
-- (Make target invokes it automatically).
-- Idempotency: marker row in after_data->'seed_generated_011' — skip if present.
-- ============================================================
SET session_replication_role = 'replica';

DO $$
DECLARE
  already_seeded BOOLEAN;
BEGIN
  SELECT EXISTS (
    SELECT 1 FROM audit_logs
    WHERE tenant_id = '00000000-0000-0000-0000-000000000001'::uuid
      AND after_data ? 'seed_generated_011'
    LIMIT 1
  ) INTO already_seeded;

  IF NOT already_seeded THEN
    INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id, after_data, hash, prev_hash, created_at)
    SELECT
      '00000000-0000-0000-0000-000000000001'::uuid,
      '00000000-0000-0000-0000-000000000010'::uuid,
      CASE (g % 14)
        WHEN 0  THEN 'imei_pool.created'
        WHEN 1  THEN 'imei_pool.updated'
        WHEN 2  THEN 'imei_pool.deleted'
        WHEN 3  THEN 'imei_pool.bulk_imported'
        WHEN 4  THEN 'imei_pool.entry_added'
        WHEN 5  THEN 'device.binding_changed'
        WHEN 6  THEN 'device.binding_re_paired'
        WHEN 7  THEN 'device.binding_grace_expiring'
        WHEN 8  THEN 'imei.changed'
        WHEN 9  THEN 'log_forwarding.destination_added'
        WHEN 10 THEN 'log_forwarding.destination_updated'
        WHEN 11 THEN 'log_forwarding.destination_disabled'
        WHEN 12 THEN 'log_forwarding.destination_deleted'
        ELSE         'log_forwarding.delivery_failed'
      END,
      CASE (g % 14)
        WHEN 0  THEN 'imei_whitelist'
        WHEN 1  THEN 'imei_whitelist'
        WHEN 2  THEN 'imei_blacklist'
        WHEN 3  THEN 'imei_blacklist'
        WHEN 4  THEN 'imei_greylist'
        WHEN 5  THEN 'sim'
        WHEN 6  THEN 'sim'
        WHEN 7  THEN 'sim'
        WHEN 8  THEN 'sim'
        WHEN 9  THEN 'syslog_destination'
        WHEN 10 THEN 'syslog_destination'
        WHEN 11 THEN 'syslog_destination'
        WHEN 12 THEN 'syslog_destination'
        ELSE         'syslog_destination'
      END,
      CASE WHEN (g % 14) IN (5,6,7,8) THEN ('00000000-0000-0000-0000-000000000700'::uuid)::text
           ELSE ('70000000-0000-0000-0000-' || LPAD((g + 100)::text, 12, '0'))::text END,
      jsonb_build_object('seed_generated_011', true, 'sequence', g),
      repeat('0', 64),
      repeat('0', 64),
      NOW() - ((g * 60) || ' minutes')::interval - ((g % 7) || ' days')::interval
    FROM generate_series(1, 220) AS g;
  END IF;
END $$;

SET session_replication_role = 'origin';

