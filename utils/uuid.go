package utils

import (
	"github.com/google/uuid"
)

func generateUUID(prefix string) string {
	return prefix + "-" + uuid.New().String()
}

func GenerateMessageID() string {
	return generateUUID("msg")
}

func GeneratePlanID() string {
	return generateUUID("plan")
}

func GenerateTaskID() string {
	return generateUUID("task")
}

func GenerateSessionID() string {
	return generateUUID("session")
}

// GenerateShortID returns the first 8 chars of a UUID for compact identifiers
// used in ChromaDB document IDs and other places where full UUIDs are overkill.
func GenerateShortID() string {
	return uuid.New().String()[:8]
}
