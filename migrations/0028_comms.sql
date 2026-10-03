-- =============================================================
-- 0028_comms.sql
-- In-app notifications, support complaints, and admin broadcasts.
--
-- notifications: per-user feed rows. Broadcasts fan out one row per
--   user with kind='broadcast' sharing a batch_id so the admin panel
--   can list them as a single message.
-- complaints: user-filed support requests (org-scoped).
-- complaint_replies: thread replies from the user or a platform admin.
-- =============================================================

-- -------------------------------------------------------------
-- notifications
-- -------------------------------------------------------------
CREATE TABLE notifications (
	id              UUID PRIMARY KEY,
	organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
	user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind            TEXT NOT NULL,
	title           TEXT NOT NULL,
	body            TEXT NOT NULL DEFAULT '',
	link            TEXT NOT NULL DEFAULT '',
	batch_id        UUID NULL,
	is_read         BOOLEAN NOT NULL DEFAULT FALSE,
	read_at         TIMESTAMPTZ NULL,
	created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT notifications_title_len CHECK (char_length(title) BETWEEN 1 AND 160),
	CONSTRAINT notifications_body_len CHECK (char_length(body) <= 2000)
);

CREATE INDEX notifications_user_feed_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_user_unread_idx ON notifications (user_id) WHERE is_read = FALSE;
CREATE INDEX notifications_batch_idx ON notifications (batch_id, created_at DESC) WHERE batch_id IS NOT NULL;

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY notifications_tenant ON notifications
	USING (organization_id = current_org_id())
	WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- complaints
-- -------------------------------------------------------------
CREATE TABLE complaints (
	id              UUID PRIMARY KEY,
	organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
	user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	subject         TEXT NOT NULL,
	message         TEXT NOT NULL,
	status          TEXT NOT NULL DEFAULT 'OPEN',
	created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	resolved_at     TIMESTAMPTZ NULL,
	CONSTRAINT complaints_subject_len CHECK (char_length(subject) BETWEEN 1 AND 160),
	CONSTRAINT complaints_message_len CHECK (char_length(message) BETWEEN 1 AND 4000),
	CONSTRAINT complaints_status CHECK (status IN ('OPEN', 'RESOLVED'))
);

CREATE INDEX complaints_org_idx ON complaints (organization_id, created_at DESC);
CREATE INDEX complaints_user_idx ON complaints (user_id, created_at DESC);

ALTER TABLE complaints ENABLE ROW LEVEL SECURITY;
ALTER TABLE complaints FORCE ROW LEVEL SECURITY;
CREATE POLICY complaints_tenant ON complaints
	USING (organization_id = current_org_id())
	WITH CHECK (organization_id = current_org_id());

-- -------------------------------------------------------------
-- complaint_replies
-- -------------------------------------------------------------
CREATE TABLE complaint_replies (
	id              UUID PRIMARY KEY,
	complaint_id    UUID NOT NULL REFERENCES complaints(id) ON DELETE CASCADE,
	organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
	author_user_id  UUID NULL REFERENCES users(id) ON DELETE SET NULL,
	author_admin_id UUID NULL REFERENCES platform_admins(id) ON DELETE SET NULL,
	body            TEXT NOT NULL,
	created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT complaint_replies_body_len CHECK (char_length(body) BETWEEN 1 AND 4000),
	CONSTRAINT complaint_replies_author CHECK (
		(author_user_id IS NOT NULL AND author_admin_id IS NULL) OR
		(author_user_id IS NULL AND author_admin_id IS NOT NULL)
	)
);

CREATE INDEX complaint_replies_thread_idx ON complaint_replies (complaint_id, created_at ASC);

ALTER TABLE complaint_replies ENABLE ROW LEVEL SECURITY;
ALTER TABLE complaint_replies FORCE ROW LEVEL SECURITY;
CREATE POLICY complaint_replies_tenant ON complaint_replies
	USING (organization_id = current_org_id())
	WITH CHECK (organization_id = current_org_id());
