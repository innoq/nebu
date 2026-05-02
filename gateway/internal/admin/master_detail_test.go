package admin

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// stubUserRows converts stubUsers to []UserRowData with pre-computed Badge fields.
// Used by Story 7.2 tests that directly construct UsersPageData.
func stubUserRows() []UserRowData {
	rows := make([]UserRowData, len(stubUsers))
	for i, u := range stubUsers {
		rows[i] = toUserRowData(u)
	}
	return rows
}

// TestUsersListRendersStubUsers verifies that the users list template renders
// all stub users when no active item is selected.
// AC: 1, 3 (Story 7.2)
func TestUsersListRendersStubUsers(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	// Use a trimmed slice of 3 users for a focused test
	threeRows := []UserRowData{
		{StubUser: StubUser{ID: "usr-001", DisplayName: "Alice Müller", Email: "a***@example.com", Role: "instance_admin", Status: "active"}, Badge: StatusBadgeData{Status: "active"}},
		{StubUser: StubUser{ID: "usr-002", DisplayName: "Bob Wagner", Email: "b***@example.com", Role: "compliance_officer", Status: "active"}, Badge: StatusBadgeData{Status: "active"}},
		{StubUser: StubUser{ID: "usr-003", DisplayName: "Carla Reiter", Email: "c***@example.com", Role: "user", Status: "active"}, Badge: StatusBadgeData{Status: "active"}},
	}

	data := UsersPageData{
		PageData:     PageData{ActiveNav: "users"},
		Users:        threeRows,
		ActiveItemID: "", // list view — no active item
		ActiveUser:   nil,
		CloseURL:     "/admin/users",
	}

	w := httptest.NewRecorder()
	h.render(w, "users", data)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}

	body := w.Body.String()
	for _, name := range []string{"Alice Müller", "Bob Wagner", "Carla Reiter"} {
		if !strings.Contains(body, name) {
			t.Errorf("expected rendered HTML to contain %q", name)
		}
	}
}

// TestUsersDetailActiveClass verifies that the selected user's list item
// receives the DaisyUI `active` class and the detail panel uses the correct
// WCAG ARIA roles.
// AC: 6, 7, 8 (Story 7.2)
func TestUsersDetailActiveClass(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	activeUser := &stubUsers[0] // usr-001 = Alice Müller
	data := UsersPageData{
		PageData:     PageData{ActiveNav: "users"},
		Users:        stubUserRows(),
		ActiveItemID: "usr-001",
		ActiveUser:   activeUser,
		CloseURL:     "/admin/users",
	}

	w := httptest.NewRecorder()
	h.render(w, "users", data)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}

	body := w.Body.String()

	// The selected user's ID must appear in the rendered HTML
	if !strings.Contains(body, "usr-001") {
		t.Error("expected usr-001 ID in rendered HTML")
	}

	// AC6: active class must appear on the selected list item — the template
	// emits "active bg-primary" only for the selected item, not for status badges.
	if !strings.Contains(body, "active bg-primary") {
		t.Error("expected 'active bg-primary' highlight class on selected list item")
	}

	// AC7: detail panel must have role="region" (WCAG)
	if !strings.Contains(body, `role="region"`) {
		t.Error(`expected role="region" on detail panel section`)
	}

	// AC7: detail panel must have aria-label="Item details" (WCAG)
	if !strings.Contains(body, `aria-label="Item details"`) {
		t.Error(`expected aria-label="Item details" on detail panel`)
	}

	// AC7: close button must have aria-label="Close detail panel" (WCAG)
	if !strings.Contains(body, `aria-label="Close detail panel"`) {
		t.Error(`expected aria-label="Close detail panel" on close button`)
	}

	// MINOR-1: close button href must point to CloseURL (not empty or hardcoded)
	if !strings.Contains(body, `href="/admin/users"`) {
		t.Error(`expected close button href="/admin/users" in rendered HTML`)
	}
}

// TestUsersDetailNotFound verifies that rendering with an unknown ActiveItemID
// produces HTTP 200 with a "not found" message inside the detail panel
// (no full-page 404).
// AC: 5 (Story 7.2)
func TestUsersDetailNotFound(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := UsersPageData{
		PageData:     PageData{ActiveNav: "users"},
		Users:        stubUserRows(),
		ActiveItemID: "nonexistent",
		ActiveUser:   nil, // not found — handler sets nil
		CloseURL:     "/admin/users",
	}

	w := httptest.NewRecorder()
	h.render(w, "users", data)

	// AC5: must be HTTP 200, not 404
	if w.Code != 200 {
		t.Errorf("expected HTTP 200 for not-found panel state, got %d", w.Code)
	}

	body := w.Body.String()

	// AC5: must contain "not found" text in the detail panel area
	if !strings.Contains(strings.ToLower(body), "not found") {
		t.Error("expected 'not found' text in detail panel for unknown ID")
	}
}

// TestRoomsDetailActiveClass verifies that the selected room's list item
// receives the `active` class and the WCAG roles are correct — mirrors
// TestUsersDetailActiveClass but for rooms.
// AC: 4, 6, 7 (Story 7.2)
func TestRoomsDetailActiveClass(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	activeRoom := &stubRooms[0] // room-001 = General
	data := RoomsPageData{
		PageData:     PageData{ActiveNav: "rooms"},
		Rooms:        toRoomRowDataSlice(stubRooms),
		ActiveItemID: "room-001",
		ActiveRoom:   activeRoom,
		CloseURL:     "/admin/rooms",
	}

	w := httptest.NewRecorder()
	h.render(w, "rooms", data)

	if w.Code != 200 {
		t.Fatalf("expected HTTP 200, got %d", w.Code)
	}

	body := w.Body.String()

	// Selected room ID must appear
	if !strings.Contains(body, "room-001") {
		t.Error("expected room-001 ID in rendered HTML")
	}

	// AC6: active class must appear on the selected list item — "active bg-primary"
	// is the template's highlight class, distinct from status badge classes.
	if !strings.Contains(body, "active bg-primary") {
		t.Error("expected 'active bg-primary' highlight class on selected room list item")
	}

	// WCAG: detail panel region
	if !strings.Contains(body, `role="region"`) {
		t.Error(`expected role="region" on detail panel section`)
	}

	// MINOR-2: rooms test must also assert aria-label attributes (WCAG AC7)
	if !strings.Contains(body, `aria-label="Item details"`) {
		t.Error(`expected aria-label="Item details" on detail panel`)
	}
	if !strings.Contains(body, `aria-label="Close detail panel"`) {
		t.Error(`expected aria-label="Close detail panel" on close button`)
	}
}

// TestRoomsDetailNotFound verifies that rendering with an unknown room ID
// produces HTTP 200 with a "not found" message inside the detail panel (AC5).
func TestRoomsDetailNotFound(t *testing.T) {
	h, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}

	data := RoomsPageData{
		PageData:     PageData{ActiveNav: "rooms"},
		Rooms:        toRoomRowDataSlice(stubRooms),
		ActiveItemID: "nonexistent",
		ActiveRoom:   nil,
		CloseURL:     "/admin/rooms",
	}

	w := httptest.NewRecorder()
	h.render(w, "rooms", data)

	if w.Code != 200 {
		t.Errorf("expected HTTP 200 for not-found panel state, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(strings.ToLower(body), "not found") {
		t.Error("expected 'not found' text in detail panel for unknown room ID")
	}
}
