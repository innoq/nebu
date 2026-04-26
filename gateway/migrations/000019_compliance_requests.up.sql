-- gateway/migrations/000019_compliance_requests.up.sql
-- compliance_requests: stores formal access requests from compliance officers (Story 5.3)
--
-- RLS design decision:
--   INSERT  — open (any authenticated DB role may insert via the app)
--   SELECT  — open (Story 5.4 will add row-level officer/admin filtering)
--   UPDATE  — open (approve/reject transitions handled in Story 5.4)
--   DELETE  — USING (false): no row may ever be deleted via the application role.
--             Data retention for audit trail; GDPR deletion deferred to Story 5.7
--             which will use a SECURITY DEFINER function to bypass RLS, analogous
--             to audit_log_purge in migration 000018.
-- Note: The nebu superuser runs with BYPASSRLS for now (FB-51-01 / Story 5.29).
--       All policies are defined here so that the restricted role introduced in 5.29
--       automatically inherits them without a follow-up migration.

CREATE TABLE compliance_requests (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_user_id TEXT        NOT NULL,
    room_id           TEXT        NOT NULL,
    time_range_start  TIMESTAMPTZ NOT NULL,
    time_range_end    TIMESTAMPTZ NOT NULL,
    justification     TEXT        NOT NULL,
    status            TEXT        NOT NULL DEFAULT 'pending',
    approver_user_id  TEXT,
    approved_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT compliance_requests_status_check
        CHECK (status IN ('pending', 'approved', 'rejected'))
);

-- Index for the pending-list query in Story 5.4 (ORDER BY created_at DESC, filtered by status).
CREATE INDEX compliance_requests_status_created_at_idx
    ON compliance_requests (status, created_at DESC);

ALTER TABLE compliance_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE compliance_requests FORCE ROW LEVEL SECURITY;

-- INSERT: application role may create new access requests.
CREATE POLICY compliance_requests_insert ON compliance_requests
    FOR INSERT WITH CHECK (true);

-- SELECT: application role may read requests (row-level officer filtering added in Story 5.4).
CREATE POLICY compliance_requests_select ON compliance_requests
    FOR SELECT USING (true);

-- UPDATE: allowed so Story 5.4 can write status transitions (pending → approved/rejected).
-- Column-level restriction (only status/approver_user_id/approved_at) will be added in 5.4
-- via a narrower CHECK expression once the approver identity is known.
CREATE POLICY compliance_requests_update ON compliance_requests
    FOR UPDATE USING (true);

-- DELETE: denied for all application roles — data must be retained for audit trail.
-- GDPR deletion is handled by Story 5.7 via a SECURITY DEFINER function.
CREATE POLICY compliance_requests_no_delete ON compliance_requests
    FOR DELETE USING (false);
