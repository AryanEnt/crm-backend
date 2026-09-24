package permissions

// Permission codes follow resource:action.
const (
	UsersView   = "users:view"
	UsersCreate = "users:create"
	UsersEdit   = "users:edit"
	UsersDelete = "users:delete"
	UsersAssign = "users:assign"
	UsersExport = "users:export"
	UsersManage = "users:manage"

	TeamsView   = "teams:view"
	TeamsCreate = "teams:create"
	TeamsEdit   = "teams:edit"
	TeamsDelete = "teams:delete"
	TeamsAssign = "teams:assign"
	TeamsManage = "teams:manage"

	RolesView   = "roles:view"
	RolesManage = "roles:manage"

	AuditView = "audit:view"

	LeadsView   = "leads:view"
	LeadsCreate = "leads:create"
	LeadsEdit   = "leads:edit"
	LeadsDelete = "leads:delete"
	LeadsAssign = "leads:assign"
	LeadsExport = "leads:export"

	CustomersView   = "customers:view"
	CustomersCreate = "customers:create"
	CustomersEdit   = "customers:edit"
	CustomersDelete = "customers:delete"
	CustomersExport = "customers:export"

	DealsView   = "deals:view"
	DealsCreate = "deals:create"
	DealsEdit   = "deals:edit"
	DealsDelete = "deals:delete"
	DealsAssign = "deals:assign"
	DealsExport = "deals:export"

	PipelinesView   = "pipelines:view"
	PipelinesManage = "pipelines:manage"

	ActivitiesView   = "activities:view"
	ActivitiesCreate = "activities:create"
	ActivitiesEdit   = "activities:edit"
	ActivitiesDelete = "activities:delete"

	DocumentsView   = "documents:view"
	DocumentsCreate = "documents:create"
	DocumentsEdit   = "documents:edit"
	DocumentsDelete = "documents:delete"

	AnalyticsView   = "analytics:view"
	AnalyticsExport = "analytics:export"

	ForecastsView = "forecasts:view"
	TargetsView   = "targets:view"
	TargetsManage = "targets:manage"

	PredictionsView   = "predictions:view"
	PredictionsManage = "predictions:manage"

	AutomationsView   = "automations:view"
	AutomationsManage = "automations:manage"

	CommunicationsView   = "communications:view"
	CommunicationsSend   = "communications:send"
	CommunicationsManage = "communications:manage"

	EmailView      = "email:view"
	EmailSend      = "email:send"
	EmailManage    = "email:manage"
	EmailConfigure = "email:configure"

	ReferralsView      = "referrals:view"
	ReferralsCreate    = "referrals:create"
	ReferralsEdit      = "referrals:edit"
	ReferralsManage    = "referrals:manage"
	ReferralsConfigure = "referrals:configure"

	SettingsView   = "settings:view"
	SettingsManage = "settings:manage"
	SystemView     = "system:view"

	CustomFieldsView   = "custom_fields:view"
	CustomFieldsManage = "custom_fields:manage"
	LeadSourcesView    = "lead_sources:view"
	LeadSourcesManage  = "lead_sources:manage"
	ActivityTypesView   = "activity_types:view"
	ActivityTypesManage = "activity_types:manage"
)

// Role codes.
const (
	RoleSuperAdmin     = "super_admin"
	RoleSalesManager   = "sales_manager" // Team Lead
	RoleSalesExecutive = "sales_executive"
	RoleSalesSupport   = "sales_support"
)

// Data scope for a permission grant (resource visibility).
type Scope string

const (
	ScopeOwn          Scope = "own"
	ScopeTeam         Scope = "team"
	ScopeOrganization Scope = "organization"
)

// Grant is a permission code with its data scope.
type Grant struct {
	Code  string
	Scope Scope
}

// ScopeFor returns the widest scope registered for a permission code.
func ScopeFor(scopes map[string]Scope, code string) Scope {
	if scopes == nil {
		return ""
	}
	return scopes[code]
}

// WidenScope returns the broader of two scopes.
func WidenScope(a, b Scope) Scope {
	rank := func(s Scope) int {
		switch s {
		case ScopeOrganization:
			return 3
		case ScopeTeam:
			return 2
		case ScopeOwn:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// ExpandImpliesScoped expands :manage into implied actions, carrying scope forward.
func ExpandImpliesScoped(grants []Grant) map[string]Scope {
	out := make(map[string]Scope, len(grants)*4)
	for _, g := range grants {
		code := g.Code
		scope := g.Scope
		if scope == "" {
			scope = ScopeOrganization
		}
		out[code] = WidenScope(out[code], scope)
		resource, action, ok := split(code)
		if !ok || action != "manage" {
			continue
		}
		for _, a := range []string{"view", "create", "edit", "delete", "assign", "export", "configure"} {
			implied := resource + ":" + a
			out[implied] = WidenScope(out[implied], scope)
		}
	}
	return out
}

// Set is a set of permission codes.
type Set map[string]struct{}

func NewSet(codes ...string) Set {
	s := make(Set, len(codes))
	for _, c := range codes {
		s[c] = struct{}{}
	}
	return s
}

func (s Set) Has(code string) bool {
	_, ok := s[code]
	return ok
}

func (s Set) HasAny(codes ...string) bool {
	for _, c := range codes {
		if s.Has(c) {
			return true
		}
	}
	return false
}

func (s Set) HasAll(codes ...string) bool {
	for _, c := range codes {
		if !s.Has(c) {
			return false
		}
	}
	return true
}

func (s Set) List() []string {
	out := make([]string, 0, len(s))
	for c := range s {
		out = append(out, c)
	}
	return out
}

// ExpandImplies treats :manage as implying other actions on the same resource.
// It does not imply provider-specific actions such as communications:send.
func ExpandImplies(codes []string) Set {
	s := NewSet(codes...)
	for code := range s {
		resource, action, ok := split(code)
		if !ok || action != "manage" {
			continue
		}
		for _, a := range []string{"view", "create", "edit", "delete", "assign", "export", "configure"} {
			s[resource+":"+a] = struct{}{}
		}
	}
	return s
}

func split(code string) (resource, action string, ok bool) {
	for i := 0; i < len(code); i++ {
		if code[i] == ':' {
			return code[:i], code[i+1:], true
		}
	}
	return "", "", false
}
