package admin

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// assertWellFormed parses the HTML fragment and fails the test if html.Parse
// returns a non-nil error (unclosed tags, malformed structure, etc.).
func assertWellFormed(t *testing.T, body string) {
	t.Helper()
	_, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Errorf("HTML is not well-formed: %v\nbody:\n%s", err, body)
	}
}

// renderPartial is a test helper that renders a named component partial
// by executing it directly on a page template set that includes all components.
// Every page template set (e.g. "users") includes all component partials,
// so we can call ExecuteTemplate with any component name on it.
//
// All component partial tests must be in package admin (same package) so they
// can access the unexported pageTmpls field on TemplateHandler.
func renderPartial(t *testing.T, h *TemplateHandler, componentName string, data any) string {
	t.Helper()
	// Get any page template set — all include components (see handler.go NewTemplateHandler)
	tmpl, ok := h.pageTmpls["users"]
	if !ok {
		t.Fatal("users template not found in pageTmpls — cannot test component partials")
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, componentName, data); err != nil {
		t.Fatalf("ExecuteTemplate(%q): %v", componentName, err)
	}
	return buf.String()
}

// ---------------------------------------------------------------------------
// C6 WizardStepper
// ---------------------------------------------------------------------------

// TestWizardStepperARIA verifies that:
//   - aria-current="step" appears exactly once (on the current step)
//   - The active step is step index 1 ("Approved")
//   - Step 0 ("Request") shows a completion indicator (✓ checkmark)
//   - Step 2 ("Download") does NOT have aria-current
//
// AC: 1 (Story 7.3)
func TestWizardStepperARIA(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := WizardStepperData{
		Steps:       []string{"Request", "Approved", "Download"},
		CurrentStep: 1,
	}

	body := renderPartial(t, h, "wizard_stepper", data)
	assertWellFormed(t, body) // F-5: HTML well-formedness

	// F-1: role="list" on the outer <ol> element (WCAG AC1)
	if !strings.Contains(body, `role="list"`) {
		t.Errorf(`expected role="list" on outer ol element; body:\n%s`, body)
	}

	// aria-current="step" must appear exactly once
	count := strings.Count(body, `aria-current="step"`)
	if count != 1 {
		t.Errorf(`expected aria-current="step" exactly once, got %d occurrences; body:\n%s`, count, body)
	}

	// aria-current="step" must be associated with the "Approved" step (index 1)
	ariaIdx := strings.Index(body, `aria-current="step"`)
	downloadIdx := strings.Index(body, "Download")
	approvedIdx := strings.Index(body, "Approved")
	if ariaIdx < 0 {
		t.Fatal(`aria-current="step" not found in output`)
	}
	if approvedIdx < 0 {
		t.Fatal(`"Approved" label not found in output`)
	}
	if downloadIdx < 0 {
		t.Fatal(`"Download" label not found in output`)
	}
	if ariaIdx > downloadIdx {
		t.Errorf(`aria-current="step" appears after "Download"; expected it to be in the "Approved" step block`)
	}

	// F-2: active step must have border-primary class (AC1 visual requirement)
	if !strings.Contains(body, "border-primary") {
		t.Errorf(`expected "border-primary" class on active step; body:\n%s`, body)
	}

	// Step 0 ("Request") must show a completed indicator
	requestIdx := strings.Index(body, "Request")
	if requestIdx < 0 {
		t.Fatal(`"Request" label not found in output`)
	}
	start := max(0, requestIdx-500)
	requestBlock := body[start : min(len(body), requestIdx+500)]
	if !strings.Contains(requestBlock, "✓") &&
		!strings.Contains(requestBlock, "&#10003;") &&
		!strings.Contains(requestBlock, "bg-success") {
		t.Errorf(`step 0 "Request" (completed) should show ✓ checkmark or bg-success class; snippet:\n%s`, requestBlock)
	}

	// Step 2 ("Download") must NOT have aria-current
	if ariaIdx > downloadIdx {
		t.Errorf(`aria-current="step" found on or after the "Download" step`)
	}
}

// TestWizardStepperCompletedSteps verifies that with CurrentStep=2,
// steps 0 and 1 are marked completed (bg-success or ✓), and step 2 is active.
//
// AC: 1 (Story 7.3)
func TestWizardStepperCompletedSteps(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := WizardStepperData{
		Steps:       []string{"Request", "Approved", "Download"},
		CurrentStep: 2,
	}

	body := renderPartial(t, h, "wizard_stepper", data)
	assertWellFormed(t, body) // F-5

	// F-1: role="list" on the outer <ol> element
	if !strings.Contains(body, `role="list"`) {
		t.Errorf(`expected role="list" on outer ol element; body:\n%s`, body)
	}

	// F-2: active step must have border-primary class
	if !strings.Contains(body, "border-primary") {
		t.Errorf(`expected "border-primary" class on active (Download) step; body:\n%s`, body)
	}

	// aria-current="step" must appear exactly once — on "Download" (step 2)
	count := strings.Count(body, `aria-current="step"`)
	if count != 1 {
		t.Errorf(`expected aria-current="step" exactly once, got %d; body:\n%s`, count, body)
	}

	// "Download" (step 2) must be the active step — aria-current appears after "Approved" in output
	ariaIdx := strings.Index(body, `aria-current="step"`)
	approvedIdx := strings.Index(body, "Approved")
	downloadIdx := strings.Index(body, "Download")
	if ariaIdx < 0 || approvedIdx < 0 || downloadIdx < 0 {
		t.Fatal("missing label or aria-current in output")
	}
	if ariaIdx < approvedIdx {
		t.Errorf(`aria-current="step" appears before "Approved"; expected it to be in the "Download" step block`)
	}

	// Steps 0 ("Request") and 1 ("Approved") must show completed indicators
	for _, label := range []string{"Request", "Approved"} {
		idx := strings.Index(body, label)
		if idx < 0 {
			t.Fatalf("%q label not found in output", label)
		}
		// Look in a range around the label for completion indicators
		start := max(0, idx-500)
		end := min(len(body), idx+500)
		block := body[start:end]
		if !strings.Contains(block, "✓") &&
			!strings.Contains(block, "&#10003;") &&
			!strings.Contains(block, "bg-success") {
			t.Errorf(`step %q (completed) should show ✓ or bg-success class; snippet:\n%s`, label, block)
		}
	}
}

// ---------------------------------------------------------------------------
// C7 ConfirmDialog
// ---------------------------------------------------------------------------

// TestConfirmDialogARIA verifies all required WCAG ARIA attributes and
// form action/hidden field rendering.
//
// AC: 2 (Story 7.3)
func TestConfirmDialogARIA(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := ConfirmDialogData{
		Title:        "Deactivate user",
		Message:      "Sure?",
		ConfirmLabel: "Deactivate",
		ConfirmClass: "btn-error",
		FormAction:   "/api/v1/admin/users/usr-001/deactivate",
		HiddenFields: map[string]string{"user_id": "usr-001"},
	}

	body := renderPartial(t, h, "confirm_dialog", data)
	assertWellFormed(t, body) // F-5

	// WCAG: role="alertdialog" required on the <dialog> element
	if !strings.Contains(body, `role="alertdialog"`) {
		t.Errorf(`expected role="alertdialog" in output; body:\n%s`, body)
	}

	// F-4: aria-labelledby must point to exact ID "confirm_dialog_title"
	if !strings.Contains(body, `aria-labelledby="confirm_dialog_title"`) {
		t.Errorf(`expected aria-labelledby="confirm_dialog_title" in output; body:\n%s`, body)
	}
	if !strings.Contains(body, `id="confirm_dialog_title"`) {
		t.Errorf(`expected id="confirm_dialog_title" on title element; body:\n%s`, body)
	}

	// F-4: aria-describedby must point to exact ID "confirm_dialog_message"
	if !strings.Contains(body, `aria-describedby="confirm_dialog_message"`) {
		t.Errorf(`expected aria-describedby="confirm_dialog_message" in output; body:\n%s`, body)
	}
	if !strings.Contains(body, `id="confirm_dialog_message"`) {
		t.Errorf(`expected id="confirm_dialog_message" on message element; body:\n%s`, body)
	}

	// F-3: form must use method="POST"
	if !strings.Contains(body, `method="POST"`) {
		t.Errorf(`expected method="POST" on dialog form; body:\n%s`, body)
	}

	// Form must POST to FormAction
	if !strings.Contains(body, `action="/api/v1/admin/users/usr-001/deactivate"`) {
		t.Errorf(`expected action="/api/v1/admin/users/usr-001/deactivate" in output; body:\n%s`, body)
	}

	// F-3: CSRF hidden field must be rendered (prevents 403 from CSRF middleware)
	if !strings.Contains(body, `name="_csrf"`) {
		t.Errorf(`expected hidden input name="_csrf" in output; body:\n%s`, body)
	}

	// User-supplied hidden field must be rendered
	if !strings.Contains(body, `name="user_id"`) {
		t.Errorf(`expected hidden input name="user_id" in output; body:\n%s`, body)
	}
	if !strings.Contains(body, `value="usr-001"`) {
		t.Errorf(`expected hidden input value="usr-001" in output; body:\n%s`, body)
	}

	// DaisyUI btn-error class on confirm button
	if !strings.Contains(body, "btn-error") {
		t.Errorf(`expected "btn-error" class on confirm button; body:\n%s`, body)
	}

	// ConfirmLabel must appear in button text
	if !strings.Contains(body, "Deactivate") {
		t.Errorf(`expected confirm label "Deactivate" in output; body:\n%s`, body)
	}
}

// ---------------------------------------------------------------------------
// C8 SearchInput
// ---------------------------------------------------------------------------

// TestSearchInputDebounce verifies correct name/value attributes and that
// the 300ms debounce script using requestSubmit() is rendered.
// Also verifies there is no <form> tag in the component output.
//
// AC: 3 (Story 7.3)
func TestSearchInputDebounce(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := SearchInputData{
		Placeholder: "Search users",
		Value:       "alice",
		ParamName:   "q",
	}

	body := renderPartial(t, h, "search_input", data)
	assertWellFormed(t, body) // F-5

	// Input must have correct name attribute matching ParamName
	if !strings.Contains(body, `name="q"`) {
		t.Errorf(`expected name="q" in output; body:\n%s`, body)
	}

	// Input must have correct value attribute
	if !strings.Contains(body, `value="alice"`) {
		t.Errorf(`expected value="alice" in output; body:\n%s`, body)
	}

	// Inline debounce script must call requestSubmit() with 300ms timeout (F-7)
	if !strings.Contains(body, "requestSubmit") {
		t.Errorf(`expected "requestSubmit" in debounce script; body:\n%s`, body)
	}
	if !strings.Contains(body, "300") {
		t.Errorf(`expected 300ms debounce timer in script; body:\n%s`, body)
	}

	// Component must NOT include a <form> tag — the page template owns the form
	if strings.Contains(body, "<form") {
		t.Errorf(`search_input component must not contain a <form> tag; body:\n%s`, body)
	}
}

// ---------------------------------------------------------------------------
// C9/C10 FilterBar
// ---------------------------------------------------------------------------

// TestFilterBarSelected verifies that the correct option is marked selected,
// non-matching options are not selected, and the auto-submit onchange is present.
//
// AC: 4 (Story 7.3)
func TestFilterBarSelected(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	// FilterBar accepts []FilterOption directly (no FilterBarData wrapper — see Dev Notes)
	data := []FilterOption{
		{
			Label:        "Status",
			ParamName:    "status",
			Options:      []string{"all", "active", "deactivated"},
			CurrentValue: "active",
		},
	}

	body := renderPartial(t, h, "filter_bar", data)
	assertWellFormed(t, body) // F-5

	// select must have name matching ParamName
	if !strings.Contains(body, `name="status"`) {
		t.Errorf(`expected name="status" on select element; body:\n%s`, body)
	}

	// "active" option must be marked selected
	if !strings.Contains(body, `value="active" selected`) &&
		!strings.Contains(body, `value="active"  selected`) &&
		!strings.Contains(body, "value=\"active\"\n selected") {
		// More flexible check: value="active" and "selected" must appear together
		activeIdx := strings.Index(body, `value="active"`)
		if activeIdx < 0 {
			t.Fatalf(`option value="active" not found in output; body:\n%s`, body)
		}
		// Look for "selected" within the next 20 chars
		snippet := body[activeIdx : min(len(body), activeIdx+20)]
		if !strings.Contains(snippet, "selected") {
			t.Errorf(`option value="active" must be marked selected; snippet around option: %q; full body:\n%s`, snippet, body)
		}
	}

	// "all" option must NOT be selected
	allIdx := strings.Index(body, `value="all"`)
	if allIdx >= 0 {
		snippet := body[allIdx : min(len(body), allIdx+20)]
		if strings.Contains(snippet, "selected") {
			t.Errorf(`option value="all" must NOT be selected; snippet: %q`, snippet)
		}
	}

	// "deactivated" option must NOT be selected
	deactivatedIdx := strings.Index(body, `value="deactivated"`)
	if deactivatedIdx >= 0 {
		snippet := body[deactivatedIdx : min(len(body), deactivatedIdx+30)]
		if strings.Contains(snippet, "selected") {
			t.Errorf(`option value="deactivated" must NOT be selected; snippet: %q`, snippet)
		}
	}

	// onchange="this.form.submit()" must be present on the <select>
	if !strings.Contains(body, `this.form.submit()`) {
		t.Errorf(`expected onchange="this.form.submit()" on select; body:\n%s`, body)
	}
}

// TestFilterBarMultipleFilters verifies that with two FilterOptions, each select
// only marks its own CurrentValue as selected — no value bleeds across filters (F-8).
// This guards the named-variable scope fix ($filter/$opt) in filter_bar.html.
func TestFilterBarMultipleFilters(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := []FilterOption{
		{Label: "Status", ParamName: "status", Options: []string{"all", "active", "deactivated"}, CurrentValue: "active"},
		{Label: "Role", ParamName: "role", Options: []string{"all", "admin", "user"}, CurrentValue: "admin"},
	}

	body := renderPartial(t, h, "filter_bar", data)
	assertWellFormed(t, body)

	// "active" must be selected in the status select
	if !strings.Contains(body, `value="active" selected`) {
		t.Errorf(`value="active" must be selected in status filter`)
	}

	// "admin" must be selected in the role select
	if !strings.Contains(body, `value="admin" selected`) {
		t.Errorf(`value="admin" must be selected in role filter`)
	}

	// "user" must NOT be selected (its CurrentValue is not "user")
	if strings.Contains(body, `value="user" selected`) {
		t.Errorf(`value="user" must NOT be selected`)
	}
}
