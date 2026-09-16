ALTER TABLE push_devices ADD COLUMN session_id UUID;
CREATE INDEX push_devices_user_session_idx ON push_devices (user_id, session_id);
