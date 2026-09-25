package automation

import (
	"context"
	"fmt"
	"strings"

	"github.com/crm/backend/pkg/apperrors"
)

func ValidateDefinition(trigger string, conditions []Condition, actions []Action) error {
	if !contains(AllowedTriggers, trigger) {
		return apperrors.Validation("unsupported WHEN trigger: " + trigger)
	}
	if len(actions) == 0 {
		return apperrors.Validation("at least one THEN action is required")
	}
	for i, c := range conditions {
		if !contains(AllowedConditionFields, c.Field) {
			return apperrors.Validation(fmt.Sprintf("condition %d: unsupported IF field %q", i+1, c.Field))
		}
		op := c.Operator
		if op == "" {
			op = OpEquals
		}
		switch op {
		case OpEquals, OpNotEquals, OpGte, OpLte, OpIn, OpContains:
		default:
			return apperrors.Validation(fmt.Sprintf("condition %d: unsupported operator %q", i+1, op))
		}
		if c.Value == nil {
			return apperrors.Validation(fmt.Sprintf("condition %d: value is required", i+1))
		}
	}
	for i, a := range actions {
		if !contains(AllowedActions, a.Type) {
			return apperrors.Validation(fmt.Sprintf("action %d: unsupported THEN type %q", i+1, a.Type))
		}
		if a.Params == nil {
			a.Params = map[string]any{}
		}
		if err := validateAction(a); err != nil {
			return apperrors.Validation(fmt.Sprintf("action %d: %s", i+1, err.Error()))
		}
	}
	return nil
}

func validateAction(a Action) error {
	p := a.Params
	switch a.Type {
	case ActionCreateTask:
		if strings.TrimSpace(asString(p["title"])) == "" {
			return fmt.Errorf("create_task requires title")
		}
	case ActionAssignUser:
		if strings.TrimSpace(asString(p["ownerUserId"])) == "" {
			return fmt.Errorf("assign_user requires ownerUserId")
		}
	case ActionChangeStage:
		if strings.TrimSpace(asString(p["stageId"])) == "" {
			return fmt.Errorf("change_stage requires stageId")
		}
		// pipelineId strongly recommended; validated at runtime against entity pipeline
	case ActionSendNotification:
		if strings.TrimSpace(asString(p["message"])) == "" {
			return fmt.Errorf("send_notification requires message")
		}
	case ActionUpdateField:
		field := strings.TrimSpace(asString(p["field"]))
		if field == "" {
			return fmt.Errorf("update_field requires field")
		}
		allowed := map[string]bool{
			"priority": true, "source": true, "notes": true, "status": true,
		}
		if !allowed[field] {
			return fmt.Errorf("update_field field %q is not allowed", field)
		}
	case ActionAddTag:
		if strings.TrimSpace(asString(p["tag"])) == "" {
			return fmt.Errorf("add_tag requires tag")
		}
	case ActionCreateActivity:
		if strings.TrimSpace(asString(p["title"])) == "" && strings.TrimSpace(asString(p["subject"])) == "" {
			return fmt.Errorf("create_activity requires title")
		}
	case ActionSendEmail:
		if strings.TrimSpace(asString(p["templateId"])) == "" {
			return fmt.Errorf("send_email requires templateId")
		}
	}
	return nil
}

// ValidateChangeStageTarget ensures the stage exists on the entity's pipeline.
func ValidateChangeStageTarget(ctx context.Context, entityPipelineID, actionPipelineID, stageID string, belongs func(ctx context.Context, stageID, pipelineID string) (bool, error)) error {
	pipe := strings.TrimSpace(actionPipelineID)
	if pipe == "" {
		pipe = strings.TrimSpace(entityPipelineID)
	}
	if pipe == "" {
		return fmt.Errorf("cannot change stage: entity has no pipeline")
	}
	if actionPipelineID != "" && entityPipelineID != "" && actionPipelineID != entityPipelineID {
		return fmt.Errorf("cannot change stage: target pipeline does not match the record's pipeline")
	}
	ok, err := belongs(ctx, stageID, pipe)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("impossible stage transition: stage is not an active stage on this pipeline")
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func BuilderCatalog() Catalog {
	return Catalog{
		Triggers: []CatalogItem{
			{Code: TriggerLeadCreated, Label: "Lead created", Description: "When a new lead is added"},
			{Code: TriggerLeadAssigned, Label: "Lead assigned", Description: "When a lead's owner changes"},
			{Code: TriggerLeadStageChanged, Label: "Lead stage changed", Description: "When a lead moves stage"},
			{Code: TriggerActivityCompleted, Label: "Activity completed", Description: "When an activity is marked complete"},
			{Code: TriggerActivityOverdue, Label: "Activity overdue", Description: "When a follow-up becomes overdue"},
			{Code: TriggerDocumentUploaded, Label: "Document uploaded", Description: "When a document file is uploaded"},
			{Code: TriggerDealInactive, Label: "Deal inactive", Description: "When a deal has no recent activity"},
			{Code: TriggerCustomerUpdated, Label: "Customer updated", Description: "When a customer record changes"},
			{Code: TriggerDealCreated, Label: "Deal created"},
			{Code: TriggerDealStageChanged, Label: "Deal stage changed"},
		},
		Conditions: []CatalogItem{
			{Code: CondPipeline, Label: "Pipeline"},
			{Code: CondStage, Label: "Stage"},
			{Code: CondOwner, Label: "Owner"},
			{Code: CondTeam, Label: "Team"},
			{Code: CondSource, Label: "Source"},
			{Code: CondAnzsco, Label: "ANZSCO"},
			{Code: CondPriority, Label: "Priority"},
			{Code: CondInactivityDays, Label: "Inactivity days"},
			{Code: CondDealValue, Label: "Deal value"},
			{Code: CondAttention, Label: "Attention", Description: "no_next_activity, overdue_next, over_sla, attention_needed, or no_recent_activity"},
		},
		Actions: []CatalogItem{
			{Code: ActionCreateTask, Label: "Create task"},
			{Code: ActionAssignUser, Label: "Assign user"},
			{Code: ActionChangeStage, Label: "Change stage"},
			{Code: ActionSendNotification, Label: "Send notification"},
			{Code: ActionUpdateField, Label: "Update field"},
			{Code: ActionAddTag, Label: "Add tag"},
			{Code: ActionCreateActivity, Label: "Create activity"},
			{Code: ActionSendEmail, Label: "Send templated email", Description: "Queue the owner's mailbox to send an email template"},
		},
		Operators: []CatalogItem{
			{Code: OpEquals, Label: "is"},
			{Code: OpNotEquals, Label: "is not"},
			{Code: OpGte, Label: "at least"},
			{Code: OpLte, Label: "at most"},
			{Code: OpIn, Label: "is one of"},
			{Code: OpContains, Label: "contains"},
		},
	}
}
