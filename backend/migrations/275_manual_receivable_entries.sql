ALTER TABLE admin_receivable_entries DROP CONSTRAINT IF EXISTS admin_receivable_entries_kind_check;
ALTER TABLE admin_receivable_entries ADD CONSTRAINT admin_receivable_entries_kind_check
    CHECK (kind IN ('credit', 'manual_credit', 'payment'));
ALTER TABLE admin_receivable_entries DROP CONSTRAINT IF EXISTS admin_receivable_entries_check;
ALTER TABLE admin_receivable_entries ADD CONSTRAINT admin_receivable_entries_check
    CHECK ((kind = 'credit' AND credit_record_id IS NOT NULL)
        OR (kind IN ('manual_credit', 'payment') AND credit_record_id IS NULL));
