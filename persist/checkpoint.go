package persist

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ---- Plan Checkpoint persistence ----

// SaveCheckpointJSON saves a versioned plan snapshot.
// Key format: "checkpoints/{planID}/{version}"
func SaveCheckpointJSON(planID string, version int, checkpoint any) error {
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint marshal: %w", err)
	}
	key := "checkpoints/" + planID + "/" + padVersion(version)
	return GetManager().persister.Save(key, data)
}

// LoadCheckpointJSON loads a specific version of a plan checkpoint.
func LoadCheckpointJSON(planID string, version int, dst any) error {
	key := "checkpoints/" + planID + "/" + padVersion(version)
	data, err := GetManager().persister.Load(key)
	if err != nil {
		return fmt.Errorf("checkpoint load: %w", err)
	}
	return json.Unmarshal(data, dst)
}

// ListCheckpoints returns all checkpoint versions for a plan, sorted ascending.
func ListCheckpoints(planID string) ([]int, error) {
	prefix := "checkpoints/" + planID
	keys, err := GetManager().persister.List(prefix)
	if err != nil {
		return nil, err
	}
	var versions []int
	for _, k := range keys {
		// k is "checkpoints/{planID}/{version}", extract version
		v, err := strconv.Atoi(k[len(prefix)+1:])
		if err == nil {
			versions = append(versions, v)
		}
	}
	return versions, nil
}

func padVersion(v int) string {
	return fmt.Sprintf("%04d", v)
}
