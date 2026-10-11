-- =============================================================
-- 0061_worker_rls_functions.sql
--
-- Background workers run as ordora_app (NOBYPASSRLS, SETUP.md) with no
-- tenant context, so Row-Level Security hides every row of outbox and
-- notification_jobs from them: the relay sees no events and the worker
-- sees no jobs. Notifications have been silently piling up
-- undispatched since RLS was forced on (0002/0003).
--
-- Fix, same pattern as list_user_memberships (0005): SECURITY DEFINER
-- functions (owned by the migration role, row_security off) that do
-- exactly what the Go tick code did, nothing more. Only ordora_app
-- may call them. No table, policy, or existing function changes.
--
-- OPERATOR NOTE: on first deploy after this migration, the relay
-- will see the whole backlog of undispatched rows at once. Either
-- let it drain (emails go out, oldest first) or mark stale rows
-- dispatched beforehand, e.g.:
--   UPDATE outbox SET dispatched_at = now()
--   WHERE dispatched_at IS NULL AND occurred_at < now() - interval '7 days';
-- =============================================================

-- -------------------------------------------------------------
-- Outbox relay: claim, enqueue, ack.
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION claim_outbox_batch(p_limit integer)
RETURNS TABLE (
    id      uuid,
    org_id  uuid,
    event   text,
    payload jsonb
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    RETURN QUERY
        WITH c AS (
            SELECT o.id FROM outbox o
            WHERE o.dispatched_at IS NULL
            ORDER BY o.occurred_at ASC
            LIMIT p_limit
            FOR UPDATE SKIP LOCKED
        )
        SELECT o.id, o.organization_id, o.event_name, o.payload
          FROM outbox o
         WHERE o.id IN (SELECT c.id FROM c);
END;
$$;

REVOKE ALL ON FUNCTION claim_outbox_batch(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION claim_outbox_batch(integer) TO ordora_app;

CREATE OR REPLACE FUNCTION enqueue_notification(
    p_org_id    uuid,
    p_kind      text,
    p_channel   text,
    p_recipient citext,
    p_payload   jsonb,
    p_key       text
)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    INSERT INTO notification_jobs
        (id, organization_id, kind, channel, recipient, payload, idempotency_key)
    VALUES (gen_random_uuid(), p_org_id, p_kind, p_channel, p_recipient, p_payload, p_key)
    ON CONFLICT (idempotency_key) DO NOTHING;
END;
$$;

REVOKE ALL ON FUNCTION enqueue_notification(uuid, text, text, citext, jsonb, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION enqueue_notification(uuid, text, text, citext, jsonb, text) TO ordora_app;

CREATE OR REPLACE FUNCTION ack_outbox(p_id uuid, p_error text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    UPDATE outbox
       SET last_error = NULLIF(p_error, ''),
           dispatched_at = now()
     WHERE id = p_id;
END;
$$;

REVOKE ALL ON FUNCTION ack_outbox(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION ack_outbox(uuid, text) TO ordora_app;

-- -------------------------------------------------------------
-- Notification worker: claim, ack.
-- -------------------------------------------------------------
CREATE OR REPLACE FUNCTION claim_notification_jobs(p_limit integer)
RETURNS TABLE (
    id        uuid,
    org_id    uuid,
    kind      text,
    channel   text,
    recipient citext,
    payload   jsonb,
    attempts  integer
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    RETURN QUERY
        WITH c AS (
            SELECT j.id FROM notification_jobs j
            WHERE j.status = 'PENDING'
              AND j.next_attempt_at <= now()
            ORDER BY j.next_attempt_at ASC
            LIMIT p_limit
            FOR UPDATE SKIP LOCKED
        )
        UPDATE notification_jobs j
           SET next_attempt_at = now() + interval '5 minutes'
          FROM c
         WHERE j.id = c.id
        RETURNING j.id, j.organization_id, j.kind, j.channel,
                  j.recipient, j.payload, j.attempts;
END;
$$;

REVOKE ALL ON FUNCTION claim_notification_jobs(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION claim_notification_jobs(integer) TO ordora_app;

CREATE OR REPLACE FUNCTION ack_notification_job(
    p_id       uuid,
    p_status   text,
    p_attempts integer,
    p_error    text,
    p_next_in  text
)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    IF p_status = 'SENT' THEN
        UPDATE notification_jobs
           SET status = 'SENT', sent_at = now(),
               attempts = attempts + 1, last_error = NULL
         WHERE id = p_id;
    ELSIF p_status = 'DEAD' THEN
        UPDATE notification_jobs
           SET status = 'DEAD', last_error = NULLIF(p_error, ''),
               attempts = attempts + 1
         WHERE id = p_id;
    ELSE
        UPDATE notification_jobs
           SET attempts = p_attempts,
               last_error = NULLIF(p_error, ''),
               next_attempt_at = now() + p_next_in::interval
         WHERE id = p_id;
    END IF;
END;
$$;

REVOKE ALL ON FUNCTION ack_notification_job(uuid, text, integer, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION ack_notification_job(uuid, text, integer, text, text) TO ordora_app;
