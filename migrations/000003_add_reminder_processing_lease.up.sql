ALTER TABLE reminders
    ADD COLUMN processing_token UUID,
    ADD COLUMN processing_lease_until TIMESTAMPTZ;

CREATE INDEX reminders_processing_lease_idx
    ON reminders (status, processing_lease_until)
    WHERE status = 'processing';
