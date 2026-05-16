package admin

// stubUsers holds fake user records for the Users master-detail page (Story 7.2).
// These are used until Epic 6 (Admin API) provides real user management endpoints.
// All data is synthetic — emails are masked, names are fictional.
var stubUsers = []StubUser{
	{ID: "usr-001", DisplayName: "Alice Müller", Email: "a***@example.com", Role: "instance_admin", Status: "active"},
	{ID: "usr-002", DisplayName: "Bob Wagner", Email: "b***@example.com", Role: "compliance_officer", Status: "active"},
	{ID: "usr-003", DisplayName: "Carla Reiter", Email: "c***@example.com", Role: "user", Status: "active"},
	{ID: "usr-004", DisplayName: "Dieter Krause", Email: "d***@example.com", Role: "user", Status: "deactivated"},
	{ID: "usr-005", DisplayName: "Eva Schneider", Email: "e***@example.com", Role: "user", Status: "active"},
	{ID: "usr-006", DisplayName: "Franz Bauer", Email: "f***@example.com", Role: "user", Status: "active"},
	{ID: "usr-007", DisplayName: "Gabi Hofmann", Email: "g***@example.com", Role: "compliance_officer", Status: "active"},
	{ID: "usr-008", DisplayName: "Hans Fischer", Email: "h***@example.com", Role: "user", Status: "deactivated"},
}

// stubRooms holds fake room records for the Rooms master-detail page (Story 7.2).
// These are used until Epic 6 (Admin API) provides real room management endpoints.
// room-006 has an intentionally empty Name to exercise the AC3 fallback template
// "(Direct Chat · N members)" added in Story 9.15.
var stubRooms = []StubRoom{
	{ID: "room-001", Name: "General", Visibility: "public", MemberCount: 47, Status: "active"},
	{ID: "room-002", Name: "Engineering", Visibility: "private", MemberCount: 12, Status: "active"},
	{ID: "room-003", Name: "Compliance-Team", Visibility: "private", MemberCount: 5, Status: "active"},
	{ID: "room-004", Name: "Old Project X", Visibility: "private", MemberCount: 8, Status: "archived"},
	{ID: "room-005", Name: "Announcements", Visibility: "public", MemberCount: 47, Status: "active"},
	{ID: "room-006", Name: "", Visibility: "private", MemberCount: 2, Status: "active"},
}

// findStubUser returns a pointer to the StubUser with the given ID,
// or nil if no match is found. Uses linear scan — acceptable for a small
// stub slice that will be replaced by an API call in Epic 6.
func findStubUser(id string) *StubUser {
	for i := range stubUsers {
		if stubUsers[i].ID == id {
			return &stubUsers[i]
		}
	}
	return nil
}

// findStubRoom returns a pointer to the StubRoom with the given ID,
// or nil if no match is found.
func findStubRoom(id string) *StubRoom {
	for i := range stubRooms {
		if stubRooms[i].ID == id {
			return &stubRooms[i]
		}
	}
	return nil
}

// stubRoomMembers holds fake member lists for the Room Detail panel (Story 9.18).
// Only rooms with active members are listed here; rooms absent from this map render
// with an empty member list (no crash, no heading rendered — {{ if .ActiveRoomMembers }} guard).
var stubRoomMembers = map[string][]RoomMemberData{
	"room-001": {
		{UserID: "usr-001", DisplayName: "Alice Müller", JoinedAt: 1714560000000},
		{UserID: "usr-003", DisplayName: "Carla Reiter", JoinedAt: 1714646400000},
	},
	"room-002": {
		{UserID: "usr-002", DisplayName: "Bob Wagner", JoinedAt: 1714560000000},
	},
}

// StubComplianceRequest is a fake compliance access request record for the Compliance page (Story 7.11).
// Used until a real compliance API is available. Status is "pending", "approved", or "rejected".
type StubComplianceRequest struct {
	ID          string
	UserID      string
	UserName    string
	RequestType string
	RequestedAt string
	Status      string // "pending" | "approved" | "rejected"
	ReviewedBy  string
}

// stubComplianceRequests holds fake compliance access requests for the Compliance page (Story 7.11).
// 2 pending, 1 approved — sufficient to exercise filter and approve/reject flows.
var stubComplianceRequests = []StubComplianceRequest{
	{ID: "cr-001", UserID: "usr-001", UserName: "Alice Müller", RequestType: "data-export", RequestedAt: "2026-04-28", Status: "pending", ReviewedBy: ""},
	{ID: "cr-002", UserID: "usr-003", UserName: "Carla Reiter", RequestType: "account-audit", RequestedAt: "2026-04-27", Status: "pending", ReviewedBy: ""},
	{ID: "cr-003", UserID: "usr-002", UserName: "Bob Wagner", RequestType: "data-export", RequestedAt: "2026-04-25", Status: "approved", ReviewedBy: "kai@example.com"},
}

// findStubComplianceRequest returns a pointer to the StubComplianceRequest with the given ID,
// or nil if no match is found. Uses linear scan — acceptable for a small stub slice.
func findStubComplianceRequest(id string) *StubComplianceRequest {
	for i := range stubComplianceRequests {
		if stubComplianceRequests[i].ID == id {
			return &stubComplianceRequests[i]
		}
	}
	return nil
}

// StubAuditEntry is a fake audit log entry for the Audit Log page (Story 7.12).
// Used until a real audit log API is available. Timestamps are ISO-8601-like strings
// so that date-range filtering can use simple string-prefix comparison.
// FormattedTime holds the pre-formatted timestamp ("2006-01-02 15:04") populated by the handler (AC13).
// BadgeClass is the DaisyUI badge color class pre-computed by auditActionBadgeClass (AC14).
type StubAuditEntry struct {
	ID            string
	Timestamp     string // ISO-8601-like, e.g. "2026-04-29T14:30:00Z"
	FormattedTime string // pre-formatted: "YYYY-MM-DD HH:mm", e.g. "2026-04-29 14:30" (AC13)
	Actor         string // email, e.g. "kai@example.com"
	Action        string // dot-notation verb, e.g. "user.deactivate"
	BadgeClass    string // DaisyUI badge color class, e.g. "badge-error" (AC14)
	TargetID      string // e.g. "usr-003"
	TargetName    string // human-readable, e.g. "Carla Reiter"
}

// stubAuditLog holds fake audit log entries for the Audit Log page (Story 7.12).
// 6 entries spanning 2026-04-28..2026-04-30 to exercise date-range filtering.
var stubAuditLog = []StubAuditEntry{
	{ID: "al-001", Timestamp: "2026-04-28T09:15:00Z", Actor: "kai@example.com", Action: "user.deactivate", TargetID: "usr-004", TargetName: "Dieter Krause"},
	{ID: "al-002", Timestamp: "2026-04-28T14:42:00Z", Actor: "kai@example.com", Action: "room.archive", TargetID: "room-004", TargetName: "Old Project X"},
	{ID: "al-003", Timestamp: "2026-04-29T10:05:00Z", Actor: "kai@example.com", Action: "config.update", TargetID: "config", TargetName: "InstanceName"},
	{ID: "al-004", Timestamp: "2026-04-29T14:30:00Z", Actor: "admin@example.com", Action: "user.role_change", TargetID: "usr-002", TargetName: "Bob Wagner"},
	{ID: "al-005", Timestamp: "2026-04-30T08:00:00Z", Actor: "kai@example.com", Action: "compliance.approve", TargetID: "cr-003", TargetName: "Bob Wagner"},
	{ID: "al-006", Timestamp: "2026-04-30T11:20:00Z", Actor: "admin@example.com", Action: "room.archive", TargetID: "room-002", TargetName: "Engineering"},
}

// StubConfig holds server-wide configuration settings for the Config page (Story 7.10).
// Used until Epic 6 (Admin API) provides PATCH /api/v1/admin/config.
type StubConfig struct {
	InstanceName          string
	AllowRegistration     bool
	MaxRoomsPerUser       int
	RetentionDays         int
	OidcDirectoryEnabled  bool   // Story 14-2a: OIDC directory feature flag
	OidcDirectoryEndpoint string // Story 14-2a: OIDC provider user-search endpoint URL
}

// stubConfig is the in-memory server config, mutated by UpdateConfigHandler (Story 7.10).
// Changes are lost on gateway restart — acceptable for stub phase.
var stubConfig = StubConfig{
	InstanceName:          "Nebu Dev",
	AllowRegistration:     true,
	MaxRoomsPerUser:       10,
	RetentionDays:         90,
	OidcDirectoryEnabled:  false,
	OidcDirectoryEndpoint: "",
}

// StubRoleMappingConfig holds OIDC group claim → role mapping configuration (Story 7.15).
// Used until Epic 6 (Admin API) provides a real persistence layer.
// OIDCGroupClaim is the OIDC claim name (e.g. "groups").
// InstanceAdminGroup is the claim value that maps to instance_admin.
// ComplianceUserGroup is the claim value that maps to compliance_user (optional).
type StubRoleMappingConfig struct {
	OIDCGroupClaim      string // claim name, e.g. "groups"
	InstanceAdminGroup  string // value that maps to instance_admin, e.g. "instance_admin"
	ComplianceUserGroup string // value that maps to compliance_user, e.g. "" (optional)
}

// stubRoleMappingConfig is the in-memory role mapping config, mutated by
// RoleMappingHandler.UpdateHandler (Story 7.15).
// Changes are lost on gateway restart — acceptable for stub phase.
var stubRoleMappingConfig = StubRoleMappingConfig{
	OIDCGroupClaim:      "groups",
	InstanceAdminGroup:  "instance_admin",
	ComplianceUserGroup: "",
}
