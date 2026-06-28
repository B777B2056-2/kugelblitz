package persist

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

func SavePlanJSON(planID string, plan any) error {
	mgr := GetManager()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("persist plan: %w", err)
	}
	return mgr.JSONL().WriteAll(context.Background(), filepath.Join("memory", "plans", planID, "plan.jsonl"), []JSONLEvent{
		{Type: "plan", Payload: data},
	})
}

func LoadPlanJSON(planID string, dst any) error {
	mgr := GetManager()
	events, err := mgr.JSONL().ReadAll(filepath.Join("memory", "plans", planID, "plan.jsonl"))
	if err != nil || len(events) == 0 {
		return fmt.Errorf("load plan: %w", err)
	}
	return json.Unmarshal(events[0].Payload, dst)
}

func ListPlans() ([]string, error) {
	mgr := GetManager()
	// Plans are stored as directories; List returns subdirectory names
	keys, err := mgr.JSONL().List(context.Background(), filepath.Join("memory", "plans"))
	if err != nil {
		return nil, err
	}
	// Strip prefix to get planIDs
	var ids []string
	for _, k := range keys {
		id := filepath.Base(k)
		ids = append(ids, id)
	}
	return ids, nil
}

func DeletePlan(planID string) error {
	mgr := GetManager()
	return mgr.JSONL().Delete(context.Background(), filepath.Join("memory", "plans", planID, "plan.jsonl"))
}
