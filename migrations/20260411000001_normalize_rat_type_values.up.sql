-- AC-9: Normalize stored rat_type values to canonical form (from internal/aaa/rattype)
-- This migration is idempotent — rows with canonical values are unaffected.
-- Canonical set: lte, nr_5g, nr_5g_nsa, nb_iot, lte_m, utran, geran, unknown

UPDATE sessions SET rat_type = 'lte'       WHERE LOWER(rat_type) IN ('4g', 'eutran', 'e-utran');
UPDATE sessions SET rat_type = 'nr_5g'     WHERE LOWER(rat_type) IN ('5g', 'nr', '5g_sa');
UPDATE sessions SET rat_type = 'nr_5g_nsa' WHERE LOWER(rat_type) IN ('5g_nsa');
UPDATE sessions SET rat_type = 'nb_iot'    WHERE LOWER(rat_type) IN ('nb-iot', 'nbiot');
UPDATE sessions SET rat_type = 'lte_m'     WHERE LOWER(rat_type) IN ('lte-m', 'cat_m1', 'cat-m1');
UPDATE sessions SET rat_type = 'utran'     WHERE LOWER(rat_type) IN ('3g');
UPDATE sessions SET rat_type = 'geran'     WHERE LOWER(rat_type) IN ('2g');

UPDATE sims SET rat_type = 'lte'       WHERE LOWER(rat_type) IN ('4g', 'eutran', 'e-utran');
UPDATE sims SET rat_type = 'nr_5g'     WHERE LOWER(rat_type) IN ('5g', 'nr', '5g_sa');
UPDATE sims SET rat_type = 'nr_5g_nsa' WHERE LOWER(rat_type) IN ('5g_nsa');
UPDATE sims SET rat_type = 'nb_iot'    WHERE LOWER(rat_type) IN ('nb-iot', 'nbiot');
UPDATE sims SET rat_type = 'lte_m'     WHERE LOWER(rat_type) IN ('lte-m', 'cat_m1', 'cat-m1');
UPDATE sims SET rat_type = 'utran'     WHERE LOWER(rat_type) IN ('3g');
UPDATE sims SET rat_type = 'geran'     WHERE LOWER(rat_type) IN ('2g');

UPDATE cdrs SET rat_type = 'lte'       WHERE LOWER(rat_type) IN ('4g', 'eutran', 'e-utran');
UPDATE cdrs SET rat_type = 'nr_5g'     WHERE LOWER(rat_type) IN ('5g', 'nr', '5g_sa');
UPDATE cdrs SET rat_type = 'nr_5g_nsa' WHERE LOWER(rat_type) IN ('5g_nsa');
UPDATE cdrs SET rat_type = 'nb_iot'    WHERE LOWER(rat_type) IN ('nb-iot', 'nbiot');
UPDATE cdrs SET rat_type = 'lte_m'     WHERE LOWER(rat_type) IN ('lte-m', 'cat_m1', 'cat-m1');
UPDATE cdrs SET rat_type = 'utran'     WHERE LOWER(rat_type) IN ('3g');
UPDATE cdrs SET rat_type = 'geran'     WHERE LOWER(rat_type) IN ('2g');
