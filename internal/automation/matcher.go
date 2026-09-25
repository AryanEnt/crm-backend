package automation

import (
	"fmt"
	"strings"
)

func MatchConditions(conditions []Condition, ctx map[string]any) (bool, string) {
	if len(conditions) == 0 {
		return true, "no conditions"
	}
	for _, c := range conditions {
		ok, detail := matchOne(c, ctx)
		if !ok {
			return false, detail
		}
	}
	return true, "all conditions matched"
}

func matchOne(c Condition, ctx map[string]any) (bool, string) {
	op := c.Operator
	if op == "" {
		op = OpEquals
	}
	actual := contextValue(c.Field, ctx)
	switch op {
	case OpEquals:
		if stringify(actual) == stringify(c.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("%s is %v, expected %v", c.Field, actual, c.Value)
	case OpNotEquals:
		if stringify(actual) != stringify(c.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("%s is %v, excluded value", c.Field, actual)
	case OpGte:
		if toFloat(actual) >= toFloat(c.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("%s=%v is below %v", c.Field, actual, c.Value)
	case OpLte:
		if toFloat(actual) <= toFloat(c.Value) {
			return true, ""
		}
		return false, fmt.Sprintf("%s=%v is above %v", c.Field, actual, c.Value)
	case OpIn:
		for _, v := range toList(c.Value) {
			if stringify(actual) == stringify(v) {
				return true, ""
			}
		}
		return false, fmt.Sprintf("%s=%v not in list", c.Field, actual)
	case OpContains:
		if strings.Contains(strings.ToLower(stringify(actual)), strings.ToLower(stringify(c.Value))) {
			return true, ""
		}
		return false, fmt.Sprintf("%s does not contain %v", c.Field, c.Value)
	default:
		return false, "unknown operator"
	}
}

func contextValue(field string, ctx map[string]any) any {
	switch field {
	case CondPipeline:
		return ctx["pipelineId"]
	case CondStage:
		return ctx["stageId"]
	case CondOwner:
		return ctx["ownerUserId"]
	case CondTeam:
		return ctx["teamId"]
	case CondSource:
		return ctx["source"]
	case CondAnzsco:
		return ctx["anzscoId"]
	case CondPriority:
		return ctx["priority"]
	case CondInactivityDays:
		return ctx["inactivityDays"]
	case CondDealValue:
		return ctx["dealValue"]
	case CondAttention:
		return ctx["attention"]
	default:
		return ctx[field]
	}
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case int:
		return fmt.Sprintf("%d", t)
	case bool:
		return fmt.Sprintf("%v", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		var f float64
		_, _ = fmt.Sscanf(t, "%f", &f)
		return f
	default:
		return 0
	}
}

func toList(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	default:
		return []any{v}
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
