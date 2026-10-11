-- =============================================================
-- 0067_outbox_retry.sql
--
-- Failed outbox dispatches must retry, not vanish.
--
-- Before: ack_outbox(id, err) always set dispatched_at, and the Go relay
-- acked twice on failure (error, then success) — poison payloads and
-- transient enqueue failures were marked dispatched with a wiped error.
-- Notifications were lost without a trace.
--
-- After: an error ack increments attempts and records last_error while
-- leaving dispatched_at NULL, so the row is retried. claim_outbox_batch
-- skips rows past the attempt cap (8, matching the notification worker's
-- MaxTries): permanently-broken payloads rest with their error visible
-- instead of spinning forever or disappearing.
-- =============================================================

CREATE OR REPLACE FUNCTION ack_outbox(p_id uuid, p_error text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
BEGIN
    SET LOCAL row_security = off;

    IF NULLIF(p_error, '') IS NULL THEN
        UPDATE outbox
           SET last_error = NULL,
               dispatched_at = now()
         WHERE id = p_id;
    ELSE
        UPDATE outbox
           SET last_error = p_error,
               attempts = attempts + 1
         WHERE id = p_id;
    END IF;
END;
$$;

REVOKE ALL ON FUNCTION ack_outbox(uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION ack_outbox(uuid, text) TO ordora_app;

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
              AND o.attempts < 8
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
