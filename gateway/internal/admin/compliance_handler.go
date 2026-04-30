package admin

import (
	"net/http"
)

// ComplianceHandler serves the /admin/compliance page (Story 7.11).
// Uses stub data until a real compliance API is available.
type ComplianceHandler struct {
	tmpl *TemplateHandler
}

// NewComplianceHandler constructs a ComplianceHandler with the given template handler.
func NewComplianceHandler(tmpl *TemplateHandler) *ComplianceHandler {
	return &ComplianceHandler{tmpl: tmpl}
}

// ListHandler handles GET /admin/compliance.
// Reads ?status= query param (default "pending") and filters stubComplianceRequests.
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

	filtered := filterComplianceRequests(stubComplianceRequests, statusFilter)

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
// Sets Status="approved" and ReviewedBy="kai@example.com" on the matching stub entry.
// PRG-redirects to /admin/compliance with a flash message.
// TODO(epic-6): replace stub mutation with Admin API call when compliance API is implemented.
func (h *ComplianceHandler) ApproveHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
// Sets Status="rejected" and ReviewedBy="kai@example.com" on the matching stub entry.
// PRG-redirects to /admin/compliance with a flash message.
// TODO(epic-6): replace stub mutation with Admin API call when compliance API is implemented.
func (h *ComplianceHandler) RejectHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
