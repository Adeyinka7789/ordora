-- =============================================================
-- 0011_payment_trigger.sql
--
-- Ensures the orders.amount_paid_minor cache is kept in sync
-- with the sum of non-reversed payments. Runs on every
-- INSERT/UPDATE/DELETE of payments.
--
-- If the trigger already exists from migration 0001, this file
-- is idempotent: it drops and recreates the trigger and function.
--
-- Also performs a one-time backfill of orders.amount_paid_minor.
-- =============================================================

DROP TRIGGER IF EXISTS trg_payments_refresh_order ON payments;
DROP FUNCTION IF EXISTS refresh_order_paid();

CREATE OR REPLACE FUNCTION refresh_order_paid()
RETURNS TRIGGER AS $$
DECLARE
    v_order_id uuid;
BEGIN
    v_order_id := COALESCE(NEW.order_id, OLD.order_id);

        UPDATE orders
       SET amount_paid_minor = COALESCE((
             SELECT SUM(amount_minor)
               FROM payments
              WHERE order_id = v_order_id
                AND reversed_by IS NULL
                AND reverses   IS NULL
           ), 0),
           updated_at = now()
     WHERE id = v_order_id;

    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_payments_refresh_order
AFTER INSERT OR UPDATE OR DELETE ON payments
FOR EACH ROW EXECUTE FUNCTION refresh_order_paid();

-- One-time backfill of existing orders.
UPDATE orders o
   SET amount_paid_minor = COALESCE((
         SELECT SUM(p.amount_minor)
           FROM payments p
          WHERE p.order_id = o.id
            AND p.reversed_by IS NULL
            AND p.reverses   IS NULL
       ), 0);
