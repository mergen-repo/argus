-- Drops paper-only roaming feature (FIX-238). Active rows lost — admin must export CSV before deploy.
DROP TABLE IF EXISTS roaming_agreements CASCADE;
