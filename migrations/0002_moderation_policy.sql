-- Existing rules exempt moderators/broadcasters and retain 60-second timeouts.
ALTER TABLE bot_moderation_rules ADD COLUMN IF NOT EXISTS exempt_role text NOT NULL DEFAULT 'moderator' CHECK (exempt_role IN ('none', 'viewer', 'moderator', 'broadcaster'));
ALTER TABLE bot_moderation_rules ADD COLUMN IF NOT EXISTS timeout_seconds integer NOT NULL DEFAULT 60 CHECK (timeout_seconds BETWEEN 1 AND 1209600);
