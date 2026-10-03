-- =============================================================
-- 0029_feature_flags.sql
-- Waffle-style feature flags: platform-global kill switches with
-- optional per-org percentage rollouts, toggled from the admin panel.
--
-- Deliberately NO RLS: like users/sessions, this is a platform-level
-- table. Reads are open to app code; writes happen only through admin
-- handlers (audited), never from tenant request paths.
-- =============================================================

CREATE TABLE feature_flags (
	flag_key        TEXT PRIMARY KEY,
	name            TEXT NOT NULL,
	description     TEXT NOT NULL DEFAULT '',
	enabled         BOOLEAN NOT NULL DEFAULT FALSE,
	rollout_percent INT NOT NULL DEFAULT 100,
	created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT feature_flags_key_format CHECK (flag_key ~ '^[a-z0-9_]{1,64}$'),
	CONSTRAINT feature_flags_percent CHECK (rollout_percent BETWEEN 0 AND 100)
);

INSERT INTO feature_flags (flag_key, name, description, enabled, rollout_percent) VALUES
	('ledger_export', 'Ledger CSV export', 'Shows the Export CSV button on Payments & Ledger.', TRUE, 100),
	('support_desk', 'Support desk', 'In-app support pages, footer link, and sidebar entry.', TRUE, 100)
ON CONFLICT DO NOTHING;
