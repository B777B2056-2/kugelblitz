package persist

import (
	"encoding/json"
	"fmt"
)

// ---- Plan persistence ----

// SavePlanJSON serializes a Plan and persists it via the global Manager.
func SavePlanJSON(planID string, plan any) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("persist plan: marshal: %w", err)
	}
	return GetManager().SavePlan(planID, data)
}

// LoadPlanJSON loads a Plan from persistence and unmarshals it into dst.
func LoadPlanJSON(planID string, dst any) error {
	data, err := GetManager().LoadPlan(planID)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("load plan: unmarshal: %w", err)
	}
	return nil
}
