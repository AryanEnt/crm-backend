package automation

// WHEN triggers (event-driven).
const (
	TriggerLeadCreated       = "lead.created"
	TriggerLeadAssigned      = "lead.assigned"
	TriggerLeadStageChanged  = "lead.stage_changed"
	TriggerActivityCompleted = "activity.completed"
	TriggerActivityOverdue   = "activity.overdue"
	TriggerDocumentUploaded  = "document.uploaded"
	TriggerDealInactive      = "deal.inactive"
	TriggerCustomerUpdated   = "customer.updated"
	TriggerDealCreated       = EventDealCreated
	TriggerDealUpdated       = EventDealUpdated
	TriggerDealStageChanged  = EventDealStageChanged
)

// IF condition fields.
const (
	CondPipeline       = "pipeline"
	CondStage          = "stage"
	CondOwner          = "owner"
	CondTeam           = "team"
	CondSource         = "source"
	CondAnzsco         = "anzsco"
	CondPriority       = "priority"
	CondInactivityDays = "inactivity_days"
	CondDealValue      = "deal_value"
	CondAttention      = "attention"
)

// THEN action types.
const (
	ActionCreateTask       = "create_task"
	ActionAssignUser       = "assign_user"
	ActionChangeStage      = "change_stage"
	ActionSendNotification = "send_notification"
	ActionUpdateField      = "update_field"
	ActionAddTag           = "add_tag"
	ActionCreateActivity   = "create_activity"
	ActionSendEmail        = "send_email"
)

// Condition operators.
const (
	OpEquals    = "eq"
	OpNotEquals = "neq"
	OpGte       = "gte"
	OpLte       = "lte"
	OpIn        = "in"
	OpContains  = "contains"
)

// Payload keys reserved by the engine.
const (
	PayloadSuppressAutomation = "_suppressAutomation"
	PayloadAutomationDepth    = "_automationDepth"
)

var AllowedTriggers = []string{
	TriggerLeadCreated, TriggerLeadAssigned, TriggerLeadStageChanged,
	TriggerActivityCompleted, TriggerActivityOverdue, TriggerDocumentUploaded,
	TriggerDealInactive, TriggerCustomerUpdated,
	TriggerDealCreated, TriggerDealUpdated, TriggerDealStageChanged,
}

var AllowedConditionFields = []string{
	CondPipeline, CondStage, CondOwner, CondTeam, CondSource,
	CondAnzsco, CondPriority, CondInactivityDays, CondDealValue, CondAttention,
}

var AllowedActions = []string{
	ActionCreateTask, ActionAssignUser, ActionChangeStage,
	ActionSendNotification, ActionUpdateField, ActionAddTag, ActionCreateActivity,
	ActionSendEmail,
}

// Condition is a single IF clause.
type Condition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

// Action is a single THEN step.
type Action struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params"`
}

// Automation is a WHEN / IF / THEN rule.
type Automation struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	IsActive    bool        `json:"isActive"`
	TriggerType string      `json:"triggerType"`
	Conditions  []Condition `json:"conditions"`
	Actions     []Action    `json:"actions"`
	CreatedBy   *string     `json:"createdBy,omitempty"`
	UpdatedBy   *string     `json:"updatedBy,omitempty"`
	CreatedAt   string      `json:"createdAt"`
	UpdatedAt   string      `json:"updatedAt"`
}

type CreateInput struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	IsActive    *bool       `json:"isActive"`
	TriggerType string      `json:"triggerType"`
	Conditions  []Condition `json:"conditions"`
	Actions     []Action    `json:"actions"`
}

type UpdateInput struct {
	Name        *string      `json:"name"`
	Description *string      `json:"description"`
	IsActive    *bool        `json:"isActive"`
	TriggerType *string      `json:"triggerType"`
	Conditions  *[]Condition `json:"conditions"`
	Actions     *[]Action    `json:"actions"`
}

// ActionResult is one THEN outcome for the run log.
type ActionResult struct {
	Type    string         `json:"type"`
	Status  string         `json:"status"` // succeeded | failed | skipped
	Detail  string         `json:"detail,omitempty"`
	Error   string         `json:"error,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Run is an execution audit row.
type Run struct {
	ID                string         `json:"id"`
	AutomationID      *string        `json:"automationId"`
	AutomationName    string         `json:"automationName"`
	JobID             *string        `json:"jobId"`
	EventID           *string        `json:"eventId"`
	TriggerType       string         `json:"triggerType"`
	ResourceType      string         `json:"resourceType"`
	ResourceID        *string        `json:"resourceId"`
	Conditions        []Condition    `json:"conditions"`
	ConditionsMatched bool           `json:"conditionsMatched"`
	Actions           []Action       `json:"actions"`
	ActionResults     []ActionResult `json:"actionResults"`
	Status            string         `json:"status"`
	ErrorMessage      string         `json:"errorMessage"`
	StartedAt         string         `json:"startedAt"`
	FinishedAt        *string        `json:"finishedAt,omitempty"`
}

// Job is a retryable queue item.
type Job struct {
	ID            string         `json:"id"`
	EventID       *string        `json:"eventId"`
	AutomationID  *string        `json:"automationId"`
	TriggerType   string         `json:"triggerType"`
	ResourceType  string         `json:"resourceType"`
	ResourceID    *string        `json:"resourceId"`
	Payload       map[string]any `json:"payload"`
	Status        string         `json:"status"`
	Attempts      int            `json:"attempts"`
	MaxAttempts   int            `json:"maxAttempts"`
	NextAttemptAt string         `json:"nextAttemptAt"`
	LastError     string         `json:"lastError"`
	CreatedAt     string         `json:"createdAt"`
}

// Catalog describes builder options for the UI.
type Catalog struct {
	Triggers   []CatalogItem `json:"triggers"`
	Conditions []CatalogItem `json:"conditions"`
	Actions    []CatalogItem `json:"actions"`
	Operators  []CatalogItem `json:"operators"`
}

type CatalogItem struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}
