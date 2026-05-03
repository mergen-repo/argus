-- F-A3 (STORY-094 Gate): defense-in-depth RLS policy for sim_imei_allowlist.
--
-- The base table (20260507000003) cannot FK directly to sims(id) because sims
-- has a composite PK (id, operator_id) due to LIST partitioning. RLS was
-- enabled on that migration but no policy was created — store-layer guards
-- + argus_app BYPASSRLS provided isolation. This migration adds an explicit
-- USING-clause policy that joins to sims by sim_id and checks the parent SIM's
-- tenant_id against app.current_tenant. Restores defense-in-depth even if
-- BYPASSRLS is ever revoked from argus_app.
--
-- Convention follows 20260412000006_rls_policies.up.sql.

CREATE POLICY sim_imei_allowlist_via_parent_sim ON sim_imei_allowlist
USING (
  EXISTS (
    SELECT 1 FROM sims s
    WHERE s.id = sim_imei_allowlist.sim_id
      AND s.tenant_id = current_setting('app.current_tenant', true)::uuid
  )
);
