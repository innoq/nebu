package admin

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// C11 InlineEdit
// ---------------------------------------------------------------------------

// TestInlineEditARIA verifies that the inline_edit component renders all
// required ARIA attributes, form fields, and CSRF token correctly.
//
// AC: 1 (Story 7.4)
func TestInlineEditARIA(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := InlineEditData{
		ID:         "display-name",
		FieldName:  "display_name",
		Value:      "Alice Müller",
		Label:      "Display Name",
		FormAction: "/admin/users/usr-001/display-name",
		CSRFToken:  "tok123",
	}

	body := renderPartial(t, h, "inline_edit", data)
	assertWellFormed(t, body)

	// Edit button must have aria-label="Edit Display Name"
	if !strings.Contains(body, `aria-label="Edit Display Name"`) {
		t.Errorf(`expected aria-label="Edit Display Name" in output; body:\n%s`, body)
	}

	// Input must have aria-label for screen reader accessibility
	if !strings.Contains(body, `aria-label="Display Name"`) {
		t.Errorf(`expected aria-label="Display Name" on input; body:\n%s`, body)
	}

	// Input must have correct field name
	if !strings.Contains(body, `name="display_name"`) {
		t.Errorf(`expected name="display_name" in output; body:\n%s`, body)
	}

	// Input must have correct value
	if !strings.Contains(body, `value="Alice Müller"`) {
		t.Errorf(`expected value="Alice Müller" in output; body:\n%s`, body)
	}

	// Form must use POST method
	if !strings.Contains(body, `method="POST"`) {
		t.Errorf(`expected method="POST" on form; body:\n%s`, body)
	}

	// Form must POST to the correct action
	if !strings.Contains(body, `action="/admin/users/usr-001/display-name"`) {
		t.Errorf(`expected action="/admin/users/usr-001/display-name" in output; body:\n%s`, body)
	}

	// CSRF hidden field must carry both name and value as a correlated pair
	if !strings.Contains(body, `name="_csrf" value="tok123"`) {
		t.Errorf(`expected name="_csrf" value="tok123" CSRF pair in output; body:\n%s`, body)
	}
}

// ---------------------------------------------------------------------------
// C12 AlertBanner
// ---------------------------------------------------------------------------

// TestAlertBannerSuccess verifies that a dismissible success banner renders
// role="alert", the correct severity class, aria-live="polite", and a dismiss button.
//
// AC: 2 (Story 7.4)
func TestAlertBannerSuccess(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := AlertBannerData{
		Severity:    "success",
		Message:     "User deactivated.",
		Dismissible: true,
	}

	body := renderPartial(t, h, "alert_banner", data)
	assertWellFormed(t, body)

	// role="alert" must be present
	if !strings.Contains(body, `role="alert"`) {
		t.Errorf(`expected role="alert" in output; body:\n%s`, body)
	}

	// Correct severity class must be present
	if !strings.Contains(body, "alert-success") {
		t.Errorf(`expected "alert-success" class in output; body:\n%s`, body)
	}

	// Dismiss button must be present (Dismissible: true)
	if !strings.Contains(body, `aria-label="Dismiss"`) {
		t.Errorf(`expected aria-label="Dismiss" for dismiss button; body:\n%s`, body)
	}

	// info/success → aria-live="polite"
	if !strings.Contains(body, `aria-live="polite"`) {
		t.Errorf(`expected aria-live="polite" for success severity; body:\n%s`, body)
	}

	// Message content must be rendered
	if !strings.Contains(body, "User deactivated.") {
		t.Errorf(`expected message text "User deactivated." in output; body:\n%s`, body)
	}
}

// TestAlertBannerWarningAssertive verifies that a non-dismissible warning banner
// renders aria-live="assertive" and does NOT render a dismiss button.
//
// AC: 2 (Story 7.4)
func TestAlertBannerWarningAssertive(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := AlertBannerData{
		Severity:    "warning",
		Message:     "Quota nearly reached.",
		Dismissible: false,
	}

	body := renderPartial(t, h, "alert_banner", data)
	assertWellFormed(t, body)

	// warning/error → aria-live="assertive"
	if !strings.Contains(body, `aria-live="assertive"`) {
		t.Errorf(`expected aria-live="assertive" for warning severity; body:\n%s`, body)
	}

	// No dismiss button when Dismissible is false
	if strings.Contains(body, `aria-label="Dismiss"`) {
		t.Errorf(`dismiss button must NOT be present when Dismissible=false; body:\n%s`, body)
	}
}

// ---------------------------------------------------------------------------
// C13 StatusBadge
// ---------------------------------------------------------------------------

// TestStatusBadgeClasses verifies that each status value maps to the correct
// DaisyUI badge class, and that role="status" is present.
//
// AC: 3 (Story 7.4)
func TestStatusBadgeClasses(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	cases := []struct {
		status    string
		wantClass string
	}{
		{"active", "badge-success"},
		{"inactive", "badge-error"},
		{"pending", "badge-warning"},
		{"unknown_status", "badge-ghost"},
	}

	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			data := StatusBadgeData{Status: tc.status}
			body := renderPartial(t, h, "status_badge", data)
			assertWellFormed(t, body)

			if !strings.Contains(body, tc.wantClass) {
				t.Errorf(`status=%q: expected class %q in output; body:\n%s`, tc.status, tc.wantClass, body)
			}

			if !strings.Contains(body, `role="status"`) {
				t.Errorf(`status=%q: expected role="status" in output; body:\n%s`, tc.status, body)
			}
		})
	}
}

// TestStatusBadgeLabelOverride verifies that when a Label is provided it is
// used as display text instead of Status.
//
// AC: 3 (Story 7.4)
func TestStatusBadgeLabelOverride(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := StatusBadgeData{
		Status: "active",
		Label:  "Online",
	}

	body := renderPartial(t, h, "status_badge", data)
	assertWellFormed(t, body)

	// Display text must be "Online" (the Label override)
	if !strings.Contains(body, "Online") {
		t.Errorf(`expected "Online" display text in output; body:\n%s`, body)
	}

	// The word "active" may appear in aria-label but must NOT appear as the visible text node
	// We check that ">active<" (direct text content) is not present
	if strings.Contains(body, ">active<") {
		t.Errorf(`display text should be "Online" not "active" when Label is set; body:\n%s`, body)
	}
}

// ---------------------------------------------------------------------------
// C14 EmptyState
// ---------------------------------------------------------------------------

// TestEmptyStateContent verifies that the empty_state component renders the
// heading in an <h3> and the description in a <p>.
//
// AC: 4 (Story 7.4)
func TestEmptyStateContent(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := EmptyStateData{
		Heading:     "No users yet",
		Description: "Adjust your search filters to find users.",
	}

	body := renderPartial(t, h, "empty_state", data)
	assertWellFormed(t, body)

	// Heading must appear inside an <h3>
	if !strings.Contains(body, "<h3") {
		t.Errorf(`expected <h3> element in output; body:\n%s`, body)
	}
	if !strings.Contains(body, "No users yet") {
		t.Errorf(`expected "No users yet" heading in output; body:\n%s`, body)
	}

	// Description must appear inside a <p>
	if !strings.Contains(body, "<p") {
		t.Errorf(`expected <p> element in output; body:\n%s`, body)
	}
	if !strings.Contains(body, "Adjust your search filters to find users.") {
		t.Errorf(`expected description text in output; body:\n%s`, body)
	}
}
