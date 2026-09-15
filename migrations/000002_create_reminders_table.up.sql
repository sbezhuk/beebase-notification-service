CREATE TABLE reminders (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    note TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    entity_type TEXT NOT NULL CHECK (entity_type IN ('apiary','hive','inspection','harvest')),
    entity_id UUID NOT NULL,
    reminder_type TEXT NOT NULL CHECK (reminder_type = 'custom'),
    source TEXT NOT NULL CHECK (source IN ('manual','system')),
    remind_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('scheduled','processing','sent','cancelled','failed')),
    cancel_reason TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX reminders_user_id_idx ON reminders (user_id);
CREATE INDEX reminders_entity_idx ON reminders (entity_type, entity_id);
CREATE INDEX reminders_due_idx ON reminders (status, next_attempt_at, remind_at) WHERE status = 'scheduled';
