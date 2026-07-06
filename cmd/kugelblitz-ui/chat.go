package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/runtime"
)

// handleChat processes a chat request via SSE streaming.
// Each call creates a fresh AgentLoop (single-turn), then archives results.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Goal == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "goal is required"})
		return
	}

	appCfg := GetConfig()
	if appCfg.Model.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "API Key 未配置"})
		return
	}

	goalPreview := req.Goal
	if len(goalPreview) > 80 {
		goalPreview = goalPreview[:80] + "…"
	}
	core.Info("chat started", "session", req.SessionID, "goal", goalPreview)

	session := s.sessions.GetOrCreate(req.SessionID)
	session.Goal = req.Goal
	session.hitlCh = make(chan string, 1)
	session.tokenReports = nil
	session.tokenTotal = TokenTotals{}
	session.turnMessages = nil
	session.turnPlans = nil
	session.turnUsage = StoredUsage{}
	session.currentPlan = nil

	// Set up AgentLoop — reuse framework session across turns via WithExistingSessionID
	opts := []runtime.AgentLoopOption{}
	if session.FrameworkSessionID != "" {
		opts = append(opts, runtime.WithExistingSessionID(session.FrameworkSessionID))
	}
	loop := runtime.NewAgentLoop(appCfg, opts...)

	// ── Cancellable context ──
	chatCtx, chatCancel := context.WithCancel(r.Context())
	session.mu.Lock()
	session.cancelFn = chatCancel
	session.mu.Unlock()
	defer func() {
		session.mu.Lock()
		session.cancelFn = nil
		session.mu.Unlock()
	}()

	// ── SSE setup ──
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	var sseMu sync.Mutex
	var capturedToolCalls []core.ToolCallDetail

	// ── Register hooks with inline callbacks (no sseModelHandler) ──
	loop.RegisterEventHooks(core.AgentEventHooks{
		OnThinkingChunk: func(id constants.AgentIdentity, chunk string) {
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "think", Data: map[string]any{"text": chunk, "identity": string(id)}})
		},
		OnReplyChunk: func(id constants.AgentIdentity, chunk string) {
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "reply", Data: map[string]any{"text": chunk, "identity": string(id)}})
		},
		OnBlockThinking: func(id constants.AgentIdentity, reasoning string) {
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "think", Data: map[string]any{"text": reasoning, "identity": string(id)}})
		},
		OnBlockReply: func(id constants.AgentIdentity, text string) {
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "reply", Data: map[string]any{"text": text, "identity": string(id)}})
		},
		OnFunctionCall: func(id constants.AgentIdentity, detail core.ToolCallDetail) {
			capturedToolCalls = append(capturedToolCalls, detail)
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "tool_call", Data: map[string]any{
				"tool_call_id": detail.ID,
				"tool_name":    detail.ToolName,
				"args":         detail.Args,
			}})
		},
		OnModelFinished: func(id constants.AgentIdentity, reason string) {
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "finished", Data: map[string]string{"reason": reason}})
		},
		OnUsageUpdated: func(id constants.AgentIdentity, usage core.Usage) {
			core.Debug("sse usage updated", "id", id, "total", usage.TotalTokens)
			session.addTokenReport(TokenReport{
				Identity: string(id),
				Input:    usage.InputTokens,
				Output:   usage.OutputTokens,
				Reason:   usage.ReasoningTokens,
				Total:    usage.TotalTokens,
			})
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "usage", Data: map[string]any{
				"identity":  string(id),
				"input":     usage.InputTokens,
				"output":    usage.OutputTokens,
				"reasoning": usage.ReasoningTokens,
				"total":     usage.TotalTokens,
			}})
		},
		OnError: func(id constants.AgentIdentity, err error) {
			core.Error("sse error", "id", id, "err", err)
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "error", Data: map[string]string{"message": err.Error()}})
		},
		OnToolCallEnd: func(id constants.AgentIdentity, result core.ToolCallResult) {
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "tool_result", Data: map[string]any{
				"tool_call_id": result.ToolCallID,
				"tool_name":    result.ToolName,
				"output":       result.Outputs,
			}})

			// Derive plan_update from tool results (no memory/working dependency)
			pu := s.derivePlanUpdate(session, result)
			if pu != nil {
				writeSSEEvent(w, flusher, SSEEvent{Event: "plan_update", Data: pu})
			}
		},
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			toolCallID := ""
			for _, tc := range capturedToolCalls {
				if tc.ToolName == "ask_human" {
					toolCallID = tc.ID
					break
				}
			}
			hitlSource := "generic"
			if session.currentPlan != nil {
				hitlSource = "planner_confirm"
			}

			session.mu.Lock()
			session.hitlWaiting = true
			session.hitlInfo = &HitlInfo{
				ToolCallID: toolCallID, Question: prompt, Reason: reason,
			}
			session.mu.Unlock()

			hitlPreview := prompt
			if len(hitlPreview) > 60 {
				hitlPreview = hitlPreview[:60] + "…"
			}
			core.Info("hitl paused", "session", session.ID, "question", hitlPreview)

			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "hitl", Data: map[string]any{
				"session_id":   session.ID,
				"tool_call_id": toolCallID,
				"question":     prompt,
				"reason":       reason,
				"source":       hitlSource,
			}})
			capturedToolCalls = nil
		},
		OnPlanRollback: func(id constants.AgentIdentity, planID string, targetVersion int, planName string) {
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "plan_rollback", Data: map[string]any{
				"plan_id": planID, "target_version": targetVersion, "plan_name": planName,
			}})
			if pu := s.currentPlanUpdate(session); pu != nil {
				writeSSEEvent(w, flusher, SSEEvent{Event: "plan_update", Data: pu})
			}
		},
		OnTaskUpdated: func(id constants.AgentIdentity, taskID, goal, status, output string) {
			sseMu.Lock()
			defer sseMu.Unlock()
			writeSSEEvent(w, flusher, SSEEvent{Event: "task_updated", Data: map[string]any{
				"task_id": taskID, "goal": goal, "status": status, "output": output,
			}})
			if pu := s.currentPlanUpdate(session); pu != nil {
				writeSSEEvent(w, flusher, SSEEvent{Event: "plan_update", Data: pu})
			}
		},
	})

	// ── Run AgentLoop ──
	loop.Run(chatCtx, req.Goal)

	// ── HITL / Done event loop ──
	for {
		select {
		case response := <-session.hitlCh:
			core.Info("hitl resuming", "session", session.ID)
			if err := loop.ResumeWithHumanResponse(response); err != nil {
				core.Error("hitl resume failed", "session", session.ID, "err", err)
				writeSSEEvent(w, flusher, SSEEvent{Event: "error", Data: map[string]string{
					"message": "HITL resume failed: " + err.Error(),
				}})
			}
		case <-loop.Done():
			// Capture framework session ID after first turn for reuse
			if session.FrameworkSessionID == "" {
				session.FrameworkSessionID = loop.SessionID()
			}

			session.mu.Lock()
			tt := session.tokenTotal
			tr := session.tokenReports
			session.mu.Unlock()

			writeSSEEvent(w, flusher, SSEEvent{Event: "token_report", Data: map[string]any{
				"identity":  "total",
				"input":     tt.Input,
				"output":    tt.Output,
				"reasoning": tt.Reasoning,
				"total":     tt.Total,
				"reports":   tr,
			}})
			writeSSEEvent(w, flusher, SSEEvent{Event: "done", Data: map[string]any{
				"session_id": session.ID,
				"usage": map[string]int64{
					"input": tt.Input, "output": tt.Output,
					"reasoning": tt.Reasoning, "total": tt.Total,
				},
			}})

			if session.currentPlan != nil {
				session.addTurnPlan(session.currentPlan.toStored())
			}

			s.sessions.ArchiveTurn(session)

			core.Info("chat completed", "session", session.ID, "total_tokens", tt.Total)
			return
		case <-r.Context().Done():
			loop.Cancel()
			return
		}
	}
}

// ── Plan derivation (from tool results, zero memory/working dependency) ──

func (s *Server) derivePlanUpdate(session *ChatSession, result core.ToolCallResult) *PlanUpdate {
	switch result.ToolName {
	case "plan_create":
		planID, _ := result.Outputs["id"].(string)
		planName, _ := result.Outputs["name"].(string)

		session.mu.Lock()
		session.currentPlan = &UIPlanState{
			PlanID: planID,
			Name:   planName,
			Status: "init",
			Tasks:  make(map[string]*UIPlanTask),
		}
		if tasksRaw, ok := result.Outputs["subtasks"].([]any); ok {
			for _, t := range tasksRaw {
				if tm, ok := t.(map[string]any); ok {
					taskID, _ := tm["id"].(string)
					taskGoal, _ := tm["goal"].(string)
					if taskID != "" {
						session.currentPlan.Tasks[taskID] = &UIPlanTask{
							ID: taskID, Goal: taskGoal, Status: "pending",
						}
					}
				}
			}
		}
		session.mu.Unlock()
		return s.currentPlanUpdate(session)

	case "confirm_plan":
		planID, _ := result.Outputs["id"].(string)
		planStatus, _ := result.Outputs["status"].(string)

		session.mu.Lock()
		if session.currentPlan != nil && session.currentPlan.PlanID == planID {
			session.currentPlan.Status = planStatus
		}
		session.mu.Unlock()
		return s.currentPlanUpdate(session)

	case "task_insert":
		taskID, _ := result.Outputs["id"].(string)
		taskGoal, _ := result.Outputs["goal"].(string)

		session.mu.Lock()
		if session.currentPlan != nil && taskID != "" {
			session.currentPlan.Tasks[taskID] = &UIPlanTask{
				ID: taskID, Goal: taskGoal, Status: "pending",
			}
		}
		session.mu.Unlock()
		return s.currentPlanUpdate(session)

	case "task_status_update":
		taskID, _ := result.Outputs["id"].(string)
		taskStatus, _ := result.Outputs["status"].(string)
		taskGoal, _ := result.Outputs["goal"].(string)

		session.mu.Lock()
		if session.currentPlan != nil && taskID != "" {
			if task, ok := session.currentPlan.Tasks[taskID]; ok {
				task.Status = taskStatus
				if taskGoal != "" {
					task.Goal = taskGoal
				}
			}
		}
		session.mu.Unlock()
		return s.currentPlanUpdate(session)

	case "task_delete":
		taskID, _ := result.Outputs["id"].(string)

		session.mu.Lock()
		if session.currentPlan != nil && taskID != "" {
			delete(session.currentPlan.Tasks, taskID)
		}
		session.mu.Unlock()
		return s.currentPlanUpdate(session)

	case "worker_spawn":
		return s.currentPlanUpdate(session)
	}

	return nil
}

func (s *Server) currentPlanUpdate(session *ChatSession) *PlanUpdate {
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.currentPlan == nil {
		return nil
	}
	cp := session.currentPlan
	tasks := make([]PlanTaskUpdate, 0, len(cp.Tasks))
	for _, t := range cp.Tasks {
		tasks = append(tasks, PlanTaskUpdate{
			ID: t.ID, Goal: t.Goal, Status: t.Status,
		})
	}
	return &PlanUpdate{
		PlanID: cp.PlanID,
		Name:   cp.Name,
		Status: cp.Status,
		Tasks:  tasks,
	}
}

func (p *UIPlanState) toStored() StoredPlan {
	tasks := make([]StoredTask, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		tasks = append(tasks, StoredTask{ID: t.ID, Goal: t.Goal, Status: t.Status})
	}
	return StoredPlan{
		PlanID: p.PlanID,
		Name:   p.Name,
		Status: p.Status,
		Tasks:  tasks,
	}
}

// ── SSE event types ──

type ChatRequest struct {
	SessionID string `json:"session_id"`
	Goal      string `json:"goal"`
}

type SSEEvent struct {
	Event string
	Data  any
}

type HitlInfo struct {
	ToolCallID string `json:"tool_call_id"`
	Question   string `json:"question"`
	Reason     string `json:"reason,omitempty"`
}

type PlanUpdate struct {
	PlanID string           `json:"plan_id"`
	Name   string           `json:"name"`
	Status string           `json:"status"`
	Tasks  []PlanTaskUpdate `json:"tasks"`
}

type PlanTaskUpdate struct {
	ID     string `json:"id"`
	Goal   string `json:"goal"`
	Status string `json:"status"`
}

type TokenReport struct {
	Identity string `json:"identity"`
	Input    int64  `json:"input"`
	Output   int64  `json:"output"`
	Reason   int64  `json:"reasoning,omitempty"`
	Total    int64  `json:"total"`
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, evt SSEEvent) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(evt.Data); err != nil {
		buf.Reset()
		buf.WriteString(`{"error":"marshal failed"}` + "\n")
	}
	data := bytes.TrimSpace(buf.Bytes())
	_, writeErr := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Event, string(data))
	if writeErr != nil {
		return
	}
	flusher.Flush()
}
