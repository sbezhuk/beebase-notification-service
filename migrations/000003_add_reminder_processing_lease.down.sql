DROP INDEX reminders_processing_lease_idx;
ALTER TABLE reminders
    DROP COLUMN processing_token,
    DROP COLUMN processing_lease_until;
