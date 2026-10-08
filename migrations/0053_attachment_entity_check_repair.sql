-- =============================================================
-- 0053_attachment_entity_check_repair.sql
--
-- 0052's drop loop matched ILIKE '%entity_type%IN%', but Postgres stores
-- IN-list predicates as `= ANY (ARRAY[...])`, so the old
-- ORDER/PAYMENT/CUSTOMER-only check survived next to the new one and
-- PRODUCT inserts still failed. This drops every stale entity_type check
-- and ensures the PRODUCT-tolerant one exists. Fully idempotent.
-- =============================================================

DO $$
DECLARE
    cname text;
BEGIN
    FOR cname IN
        SELECT conname FROM pg_constraint
         WHERE conrelid = 'attachments'::regclass
           AND contype = 'c'
           AND conname <> 'attachments_entity_type_product_check'
           AND pg_get_constraintdef(oid) ILIKE '%entity_type%'
    LOOP
        EXECUTE format('ALTER TABLE attachments DROP CONSTRAINT %I', cname);
    END LOOP;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conrelid = 'attachments'::regclass
           AND conname = 'attachments_entity_type_product_check'
    ) THEN
        ALTER TABLE attachments
            ADD CONSTRAINT attachments_entity_type_product_check
            CHECK (entity_type IN ('ORDER', 'PAYMENT', 'CUSTOMER', 'PRODUCT'));
    END IF;
END
$$;
