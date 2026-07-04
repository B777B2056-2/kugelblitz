// Package config provides the unified configuration for AgentLoop and Kernel.
package config

import "github.com/B777B2056-2/kugelblitz/core"

// ModelConfig groups LLM provider and thinking configuration.
type ModelConfig struct {
	Provider        core.ILMProvider
	StreamMode      bool
	EnableThinking  bool
	ReasoningEffort string
}

// ContextCompressConfig groups session memory compression parameters.
type ContextCompressConfig struct {
	MaxAttempts          int // max retries when context exceeded
	MaxToolResultChars   int // 0 = disable; >0 = compress tool results exceeding this many UTF-8 chars
	KeepLastN            int // keep the most recent N messages uncompressed
	MinMessagesToCompress int // minimum old messages needed before compression triggers
}

// TargetDriftConfig groups reviewer / drift detection parameters.
type TargetDriftConfig struct {
	ReviewInterval          int // check drift every N state machine steps
	MaxFailuresBeforeReview int // check drift after N task failures
}

// RuntimeConfig groups state machine runtime parameters.
type RuntimeConfig struct {
	MaxStateMachineCycles int
}

// Config is the top-level configuration for AgentLoop + Kernel.
type Config struct {
	Model           ModelConfig
	Runtime         RuntimeConfig
	ContextCompress ContextCompressConfig
	TargetDrift     TargetDriftConfig
	Hooks           core.AgentEventHooks
}
