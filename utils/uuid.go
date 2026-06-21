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
