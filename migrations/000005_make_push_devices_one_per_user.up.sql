CREATE TABLE push_devices_legacy (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    session_id UUID,
    destination TEXT NOT NULL,
    platform TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archive_reason TEXT NOT NULL
);

WITH ranked AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY (session_id IS NOT NULL) DESC, updated_at DESC, id DESC) AS row_number
    FROM push_devices
)
INSERT INTO push_devices_legacy (id, user_id, session_id, destination, platform, created_at, updated_at, archive_reason)
SELECT p.id, p.user_id, p.session_id, p.destination, p.platform, p.created_at, p.updated_at, 'duplicate_user_registration'
FROM push_devices p JOIN ranked r ON r.id = p.id
WHERE r.row_number > 1;

WITH ranked AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY (session_id IS NOT NULL) DESC, updated_at DESC, id DESC) AS row_number
    FROM push_devices
)
DELETE FROM push_devices p USING ranked r WHERE p.id = r.id AND r.row_number > 1;

ALTER TABLE push_devices ADD COLUMN session_generation BIGINT NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX push_devices_user_uidx ON push_devices (user_id);
DROP INDEX push_devices_user_session_idx;
CREATE INDEX push_devices_user_session_idx ON push_devices (user_id, session_id, session_generation);
