package utils

import (
	"github.com/google/uuid"
)

func generateUUID(prefix string) string {
	return prefix + "-" + uuid.New().String()
}

// GenerateMessageID returns a new random message ID.
func GenerateMessageID() string {
	return generateUUID("msg")
}

// GeneratePlanID returns a new random plan ID.
func GeneratePlanID() string {
	return generateUUID("plan")
}

// GenerateTaskID returns a new random task ID.
func GenerateTaskID() string {
	return generateUUID("task")
}

// GenerateSessionID returns a new random session ID.
func GenerateSessionID() string {
	return generateUUID("session")
}

// GenerateShortID returns the first 8 chars of a UUID for compact identifiers
// used in ChromaDB document IDs and other places where full UUIDs are overkill.
func GenerateShortID() string {
	return uuid.New().String()[:8]
}
