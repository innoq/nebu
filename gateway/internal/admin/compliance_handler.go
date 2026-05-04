package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	auditpkg "github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// ComplianceApprovalClient is a minimal consumer-defined interface for
// approving and rejecting compliance access requests from the Admin UI.
//
// The admin UI uses session-based auth (instance_admin) rather than JWT
// compliance_officer tokens. This interface performs the DB update + audit
// emission directly, bypassing the JWT role gate — correct because the admin
// session guard already verified the caller is instance_admin.
//
// Implement with DBComplianceApprovalClient for production, or a fake for tests.
type ComplianceApprovalClient interface {
	// Approve updates compliance_requests status to "approved" for the given
	// requestID. approverSub is the admin's user sub (for audit attribution).
	// Returns an error if the request is not found, already decided, or DB fails.
	Approve(ctx context.Context, requestID, approverSub, note string) error

	// Reject updates compliance_requests status to "rejected" for the given
	// requestID. approverSub is the admin's user sub (for audit attribution).
	// Returns an error if the request is not found, already decided, or DB fails.
	Reject(ctx context.Context, requestID, approverSub, note string) error

	// ListPending returns compliance requests matching statusFilter from the real DB.
	// statusFilter "all" (or empty) returns all requests.
	ListPending(ctx context.Context, statusFilter string) ([]StubComplianceRequest, error)
}

// auditComplianceTimeout caps the gRPC call for audit log emission so a hanging
// Core does not block the admin compliance request response path.
const auditComplianceTimeout = 500 * time.Millisecond

// errComplianceNotFound is returned when the requestID is not found in the DB.
var errComplianceNotFound = errors.New("compliance request not found")

// errComplianceNotPending is returned when the request is not in pending status.
var errComplianceNotPending = errors.New("compliance request is not pending")

// errComplianceSelfDecision is returned when an admin attempts to approve or
// reject a compliance request whose requester is the admin themselves.
// Four-eyes principle: the requester and approver MUST be distinct identities.
// Mirrors the guard in gateway/internal/compliance/handler.go (Story 5.4).
var errComplianceSelfDecision = errors.New("self-approval is not permitted")

// errComplianceNoteTooLong is returned when the rejection reason / note exceeds
// the 4096-character cap. Matches the cap enforced by the JWT-gated compliance
// API (gateway/internal/compliance/handler.go, FB-54-01) so the admin UI path
// cannot smuggle longer payloads into the audit log via the metadata field.
var errComplianceNoteTooLong = errors.New("note exceeds maximum length of 4096 characters")

// maxComplianceNoteLen mirrors gateway/internal/compliance/handler.go (FB-54-01).
const maxComplianceNoteLen = 4096

// DBComplianceApprovalClient implements ComplianceApprovalClient using a
// direct PostgreSQL connection and gRPC CoreClient for audit emission.
// Bypasses the JWT compliance_officer role gate — the admin session guard
// already verifies the caller is instance_admin.
type DBComplianceApprovalClient struct {
	DB         *sql.DB
	CoreClient pb.CoreServiceClient
}

// Approve implements ComplianceApprovalClient.Approve.
// Atomically transitions compliance_requests.status from 'pending' to 'approved'.
// Emits a compliance_access_approved audit event (never-raise, 500ms timeout).
func (c *DBComplianceApprovalClient) Approve(ctx context.Context, requestID, approverSub, note string) error {
	return c.decide(ctx, requestID, approverSub, note, "approved", "compliance_access_approved")
}

// Reject implements ComplianceApprovalClient.Reject.
// Atomically transitions compliance_requests.status from 'pending' to 'rejected'.
// Emits a compliance_access_rejected audit event (never-raise, 500ms timeout).
func (c *DBComplianceApprovalClient) Reject(ctx context.Context, requestID, approverSub, note string) error {
	return c.decide(ctx, requestID, approverSub, note, "rejected", "compliance_access_rejected")
}

// decide is the shared implementation for Approve and Reject.
// newStatus is "approved" or "rejected"; auditAction is the audit event name.
//
// Security invariants enforced here (must match gateway/internal/compliance/handler.go):
//   - approverSub MUST be non-empty (the session guard puts the admin's sub into
//     the request context; an empty sub means the upstream auth chain failed).
//   - len(note) ≤ maxComplianceNoteLen (4096) so the metadata payload cannot be
//     used to inflate the audit row beyond the API-side cap.
//   - approverSub MUST differ from compliance_requests.requester_user_id —
//     four-eyes principle: a compliance officer cannot approve their own request,
//     even via the admin UI session path that bypasses the JWT role gate.
func (c *DBComplianceApprovalClient) decide(ctx context.Context, requestID, approverSub, note, newStatus, auditAction string) error {
	// Defence-in-depth: refuse to write an audit row attributed to "" — that
	// would silently fail the four-eyes attribution test (any requester would
	// pass the inequality check below against an empty approver sub).
	if approverSub == "" {
		return fmt.Errorf("compliance: empty approver sub — admin session guard did not populate sub claim")
	}
	if len(note) > maxComplianceNoteLen {
		return errComplianceNoteTooLong
	}

	// Step 1: Pre-flight — verify the row exists, is pending, and that the
	// approver is NOT the original requester (four-eyes principle, Story 5.4).
	var currentStatus, requesterUserID string
	err := c.DB.QueryRowContext(ctx,
		`SELECT status, requester_user_id FROM compliance_requests WHERE id = $1`,
		requestID,
	).Scan(&currentStatus, &requesterUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", errComplianceNotFound, requestID)
	}
	if err != nil {
		return fmt.Errorf("compliance: pre-flight status check failed: %w", err)
	}
	if currentStatus != "pending" {
		return fmt.Errorf("%w: current status is %q", errComplianceNotPending, currentStatus)
	}
	if requesterUserID == approverSub {
		return fmt.Errorf("%w: requester=%s approver=%s", errComplianceSelfDecision, requesterUserID, approverSub)
	}

	// Step 2: Atomic status transition: UPDATE WHERE status='pending' RETURNING id.
	var returnedID string
	err = c.DB.QueryRowContext(ctx,
		`UPDATE compliance_requests
		    SET status = $3, approver_user_id = $2, approved_at = NOW()
		  WHERE id = $1 AND status = 'pending'
		 RETURNING id`,
		requestID, approverSub, newStatus,
	).Scan(&returnedID)
	if errors.Is(err, sql.ErrNoRows) {
		// Race condition: another reviewer approved/rejected between pre-flight and UPDATE.
		return fmt.Errorf("%w: concurrent decision race on %s", errComplianceNotPending, requestID)
	}
	if err != nil {
		return fmt.Errorf("compliance: UPDATE failed for %s: %w", requestID, err)
	}

	// Step 3: Audit — never-raise, 500ms timeout.
	if c.CoreClient != nil {
		auditCtx, cancel := context.WithTimeout(ctx, auditComplianceTimeout)
		defer cancel()
		_ = auditpkg.LogEvent(auditCtx, c.CoreClient, approverSub,
			auditAction, "compliance_request", requestID,
			map[string]any{"note": note},
			"success", "")
	}

	return nil
}

// ListPending implements ComplianceApprovalClient.ListPending.
// Queries compliance_requests filtered by statusFilter and returns them as
// []StubComplianceRequest for template compatibility.
func (c *DBComplianceApprovalClient) ListPending(ctx context.Context, statusFilter string) ([]StubComplianceRequest, error) {
	var rows *sql.Rows
	var err error

	if statusFilter == "" || statusFilter == "all" {
		rows, err = c.DB.QueryContext(ctx,
			`SELECT id, requester_user_id, room_id, justification, created_at, status,
			        COALESCE(approver_user_id, '')
			   FROM compliance_requests
			  ORDER BY created_at DESC
			  LIMIT 200`,
		)
	} else {
		rows, err = c.DB.QueryContext(ctx,
			`SELECT id, requester_user_id, room_id, justification, created_at, status,
			        COALESCE(approver_user_id, '')
			   FROM compliance_requests
			  WHERE status = $1
			  ORDER BY created_at DESC
			  LIMIT 200`,
			statusFilter,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("compliance: list query failed: %w", err)
	}
	defer rows.Close()

	var result []StubComplianceRequest
	for rows.Next() {
		var id, requesterID, roomID, justification, approverID string
		var status string
		var createdAt interface{}
		if err := rows.Scan(&id, &requesterID, &roomID, &justification, &createdAt, &status, &approverID); err != nil {
			return nil, fmt.Errorf("compliance: scan row failed: %w", err)
		}
		// Format createdAt as a display string (date only for UI).
		var requestedAt string
		switch t := createdAt.(type) {
		case time.Time:
			requestedAt = t.UTC().Format("2006-01-02")
		case string:
			if len(t) >= 10 {
				requestedAt = t[:10]
			} else {
				requestedAt = t
			}
		}
		result = append(result, StubComplianceRequest{
			ID:          id,
			UserID:      requesterID,
			UserName:    requesterID, // DisplayName not stored in compliance_requests; use sub as fallback
			RequestType: roomID,      // compliance_requests stores room_id, not a "request type" — show room
			RequestedAt: requestedAt,
			Status:      status,
			ReviewedBy:  approverID,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("compliance: rows.Err: %w", err)
	}
	return result, nil
}

// ComplianceHandler serves the /admin/compliance page (Story 7.11).
// Story 9.5: backed by real DB calls when svc is non-nil.
// Falls back to stub data when svc is nil (unit-test path — backward compatible).
type ComplianceHandler struct {
	tmpl *TemplateHandler
	svc  ComplianceApprovalClient
}

// NewComplianceHandler constructs a ComplianceHandler with the given template handler.
// Pass a non-nil svc to use the real compliance DB (production path, Story 9.5).
// Pass nil (or omit) to fall back to stub data (unit-test / backward-compatible path).
func NewComplianceHandler(tmpl *TemplateHandler, svc ...ComplianceApprovalClient) *ComplianceHandler {
	var c ComplianceApprovalClient
	if len(svc) > 0 {
		c = svc[0]
	}
	return &ComplianceHandler{tmpl: tmpl, svc: c}
}

// ListHandler handles GET /admin/compliance.
// When svc is set: fetches requests from the real DB via svc.ListPending.
// When svc is nil: falls back to stubComplianceRequests (backward-compatible for unit tests).
// Reads ?status= query param (default "pending") and filters accordingly.
// Renders compliance.html with CompliancePageData.
func (h *ComplianceHandler) ListHandler(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	if statusFilter == "" {
		statusFilter = "pending"
	}

	var flash AlertBannerData
	if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != "" {
		flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
	}

	var filtered []StubComplianceRequest

	if h.svc != nil {
		// --- real DB path ---
		items, err := h.svc.ListPending(r.Context(), statusFilter)
		if err != nil {
			slog.Warn("admin: compliance ListPending failed", "err", err)
			// Render empty list with error — do not crash.
		} else {
			filtered = items
		}
		if filtered == nil {
			filtered = []StubComplianceRequest{}
		}
	} else {
		// --- stub fallback (nil svc, unit-test path) ---
		filtered = filterComplianceRequests(stubComplianceRequests, statusFilter)
	}

	data := CompliancePageData{
		PageData:     PageData{ActiveNav: "compliance", CSRFToken: CSRFTokenFromContext(r.Context())},
		Requests:     filtered,
		StatusFilter: statusFilter,
		Flash:        flash,
		EmptyState:   EmptyStateData{Heading: "No compliance requests", Description: "No requests match the current filter. Try selecting a different status."},
		// CurrentStep=1 ("Under Review") always — demonstrates the four-eyes flow (MVP scope).
		Stepper: WizardStepperData{
			Steps:       []string{"Requested", "Under Review", "Decision"},
			CurrentStep: 1,
		},
	}
	h.tmpl.render(w, "compliance", data)
}

// ApproveHandler handles POST /admin/compliance/{id}/approve.
// Story 9.5: when svc is set, calls svc.Approve to persist the decision in
// PostgreSQL and emit a compliance_access_approved audit event.
// Falls back to stub mutation when svc is nil (unit-test path).
// PRG-redirects to /admin/compliance with a flash message.
func (h *ComplianceHandler) ApproveHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if h.svc != nil {
		// --- real DB path ---
		approverSub := AdminSubFromContext(r.Context())
		// Enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), approverSub)
		if err := h.svc.Approve(grpcCtx, id, approverSub, ""); err != nil {
			if errors.Is(err, errComplianceNotFound) {
				http.NotFound(w, r)
				return
			}
			if errors.Is(err, errComplianceNotPending) {
				// Request already decided — redirect with informational flash.
				http.Redirect(w, r, "/admin/compliance?flash=Already+decided", http.StatusFound)
				return
			}
			if errors.Is(err, errComplianceSelfDecision) {
				// Four-eyes principle: do not surface the requester sub in the flash.
				slog.Warn("admin: blocked self-approval attempt", "id", id, "approver_sub", approverSub)
				http.Redirect(w, r, "/admin/compliance?flash=Self-approval+is+not+permitted", http.StatusFound)
				return
			}
			slog.Warn("admin: compliance approve failed", "id", id, "err", err)
			http.Redirect(w, r, "/admin/compliance?flash=Error+approving+request", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/admin/compliance?flash=Approved", http.StatusFound)
		return
	}

	// --- stub fallback (nil svc, unit-test path) ---
	cr := findStubComplianceRequest(id)
	if cr == nil {
		http.NotFound(w, r)
		return
	}
	cr.Status = "approved"
	cr.ReviewedBy = "kai@example.com"
	http.Redirect(w, r, "/admin/compliance?flash=Approved", http.StatusFound)
}

// RejectHandler handles POST /admin/compliance/{id}/reject.
// Story 9.5: when svc is set, calls svc.Reject to persist the decision in
// PostgreSQL and emit a compliance_access_rejected audit event.
// Falls back to stub mutation when svc is nil (unit-test path).
// PRG-redirects to /admin/compliance with a flash message.
func (h *ComplianceHandler) RejectHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if h.svc != nil {
		// --- real DB path ---
		approverSub := AdminSubFromContext(r.Context())
		// Enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), approverSub)
		// Read optional rejection reason from form (for future template enhancement).
		// Length is validated server-side in DBComplianceApprovalClient.decide so the
		// audit metadata cannot exceed maxComplianceNoteLen (4096); we also bail out
		// here if the form payload is unparseable so the admin sees a real error
		// rather than a stale-state redirect.
		if err := r.ParseForm(); err != nil {
			slog.Warn("admin: compliance reject form parse failed", "id", id, "err", err)
			http.Redirect(w, r, "/admin/compliance?flash=Error+rejecting+request", http.StatusFound)
			return
		}
		note := r.FormValue("rejection_reason")
		if err := h.svc.Reject(grpcCtx, id, approverSub, note); err != nil {
			if errors.Is(err, errComplianceNotFound) {
				http.NotFound(w, r)
				return
			}
			if errors.Is(err, errComplianceNotPending) {
				http.Redirect(w, r, "/admin/compliance?flash=Already+decided", http.StatusFound)
				return
			}
			if errors.Is(err, errComplianceSelfDecision) {
				slog.Warn("admin: blocked self-rejection attempt", "id", id, "approver_sub", approverSub)
				http.Redirect(w, r, "/admin/compliance?flash=Self-approval+is+not+permitted", http.StatusFound)
				return
			}
			if errors.Is(err, errComplianceNoteTooLong) {
				http.Redirect(w, r, "/admin/compliance?flash=Rejection+reason+is+too+long", http.StatusFound)
				return
			}
			slog.Warn("admin: compliance reject failed", "id", id, "err", err)
			http.Redirect(w, r, "/admin/compliance?flash=Error+rejecting+request", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/admin/compliance?flash=Rejected", http.StatusFound)
		return
	}

	// --- stub fallback (nil svc, unit-test path) ---
	cr := findStubComplianceRequest(id)
	if cr == nil {
		http.NotFound(w, r)
		return
	}
	cr.Status = "rejected"
	cr.ReviewedBy = "kai@example.com"
	http.Redirect(w, r, "/admin/compliance?flash=Rejected", http.StatusFound)
}

// filterComplianceRequests returns a filtered slice of StubComplianceRequest.
// statusFilter "all" (or empty) returns all requests unfiltered.
// Used only on the stub path (svc == nil).
func filterComplianceRequests(requests []StubComplianceRequest, statusFilter string) []StubComplianceRequest {
	if statusFilter == "" || statusFilter == "all" {
		result := make([]StubComplianceRequest, len(requests))
		copy(result, requests)
		return result
	}
	result := make([]StubComplianceRequest, 0, len(requests))
	for _, cr := range requests {
		if cr.Status == statusFilter {
			result = append(result, cr)
		}
	}
	return result
}
