DROP INDEX push_devices_user_session_idx;
DROP INDEX push_devices_user_uidx;
ALTER TABLE push_devices DROP COLUMN session_generation;
CREATE INDEX push_devices_user_session_idx ON push_devices (user_id, session_id);
DROP TABLE push_devices_legacy;
