DROP TRIGGER IF EXISTS redeem_credit_source ON redeem_codes;
CREATE TRIGGER redeem_credit_source BEFORE INSERT ON redeem_codes
    FOR EACH ROW EXECUTE FUNCTION set_redeem_credit_source();
