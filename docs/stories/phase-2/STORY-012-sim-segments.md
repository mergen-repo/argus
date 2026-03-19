# STORY-012: SIM Segments & Group-First UX

## User Story
As a SIM manager, I want to save filter combinations as segments and perform bulk actions on them, so that I can manage millions of SIMs efficiently.

## Architecture Reference
- Services: SVC-03 (Core API)
- API Endpoints: API-060 to API-062
- Database Tables: TBL-10 (sims), TBL-25 (sim_segments — new table: id UUID PK, tenant_id FK, name VARCHAR, filter_definition JSONB, created_by UUID FK, created_at TIMESTAMPTZ)

## Screen Reference
- SCR-020: SIM List — segment dropdown, saved segments, bulk actions bar

## Acceptance Criteria
- [ ] POST /api/v1/sim-segments creates segment with filter definition (operator, state, apn, rat_type combos)
- [ ] GET /api/v1/sim-segments/:id/count returns count matching segment (async count, <5s for 10M SIMs)
- [ ] Segment filter executes as optimized SQL using indexes
- [ ] Bulk action toolbar appears when SIMs selected (multi-select or segment-wide)
- [ ] Summary cards show counts per state (active, suspended, etc.) filtered by current segment

## Dependencies
- Blocked by: STORY-011 (SIM CRUD)
- Blocks: STORY-013 (bulk import uses segments)

## Effort Estimate
- Size: M
- Complexity: Medium
