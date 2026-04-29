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
var stubRooms = []StubRoom{
	{ID: "room-001", Name: "General", Visibility: "public", MemberCount: 47, Status: "active"},
	{ID: "room-002", Name: "Engineering", Visibility: "private", MemberCount: 12, Status: "active"},
	{ID: "room-003", Name: "Compliance-Team", Visibility: "private", MemberCount: 5, Status: "active"},
	{ID: "room-004", Name: "Old Project X", Visibility: "private", MemberCount: 8, Status: "archived"},
	{ID: "room-005", Name: "Announcements", Visibility: "public", MemberCount: 47, Status: "active"},
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

// StubConfig holds server-wide configuration settings for the Config page (Story 7.10).
// Used until Epic 6 (Admin API) provides PATCH /api/v1/admin/config.
type StubConfig struct {
	InstanceName      string
	AllowRegistration bool
	MaxRoomsPerUser   int
	RetentionDays     int
}

// stubConfig is the in-memory server config, mutated by UpdateConfigHandler (Story 7.10).
// Changes are lost on gateway restart — acceptable for stub phase.
var stubConfig = StubConfig{
	InstanceName:      "Nebu Dev",
	AllowRegistration: true,
	MaxRoomsPerUser:   10,
	RetentionDays:     90,
}
