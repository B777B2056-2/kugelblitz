package persist

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
)

func SaveCheckpointJSON(planID string, version int, checkpoint any) error {
	mgr := GetManager()
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint marshal: %w", err)
	}
	return mgr.JSONL().WriteAll(context.Background(),
		filepath.Join("memory", "plans", planID, "checkpoints", padVersion(version)+".jsonl"),
		[]JSONLEvent{{Type: "checkpoint", Payload: data}},
	)
}

func LoadCheckpointJSON(planID string, version int, dst any) error {
	mgr := GetManager()
	events, err := mgr.JSONL().ReadAll(filepath.Join("memory", "plans", planID, "checkpoints", padVersion(version)+".jsonl"))
	if err != nil {
		return fmt.Errorf("checkpoint load: %w", err)
	}
	if len(events) == 0 {
		return fmt.Errorf("checkpoint load: no events")
	}
	return json.Unmarshal(events[0].Payload, dst)
}

func ListCheckpoints(planID string) ([]int, error) {
	mgr := GetManager()
	names, err := mgr.JSONL().List(context.Background(), filepath.Join("memory", "plans", planID, "checkpoints"))
	if err != nil {
		return nil, err
	}
	var versions []int
	for _, name := range names {
		// List returns full prefixed paths; Atoi on the whole path always fails,
		// so take only the basename (e.g. "0001") (P10).
		v, err := strconv.Atoi(filepath.Base(name))
		if err == nil {
			versions = append(versions, v)
		}
	}
	sort.Ints(versions)
	return versions, nil
}

func padVersion(v int) string {
	return fmt.Sprintf("%04d", v)
}
