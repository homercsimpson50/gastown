package feed

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/vlogs"
)

// AgentsSource polls VictoriaLogs for agent conversation events
// and emits summarized feed Events.
type AgentsSource struct {
	client  *vlogs.Client
	events  chan Event
	cancel  context.CancelFunc
	session string // optional session filter
	healthy bool   // true once VLogs responds successfully
}

// NewAgentsSource creates a source that polls VictoriaLogs for agent events.
// session is an optional filter (e.g., "gastown-mayor"); empty string means all agents.
func NewAgentsSource(session string) (*AgentsSource, error) {
	client := vlogs.New()
	ctx, cancel := context.WithCancel(context.Background())

	source := &AgentsSource{
		client:  client,
		events:  make(chan Event, 200),
		cancel:  cancel,
		session: session,
	}

	go source.poll(ctx)
	return source, nil
}

// poll periodically queries VictoriaLogs and emits new events.
func (s *AgentsSource) poll(ctx context.Context) {
	defer close(s.events)

	// Load initial events (last 5 minutes)
	s.fetchAndEmit(ctx, 5*time.Minute, 200)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Fetch events from last 5 seconds (with overlap for safety)
			s.fetchAndEmit(ctx, 5*time.Second, 50)
		}
	}
}

// fetchAndEmit queries VictoriaLogs and converts results to feed events.
func (s *AgentsSource) fetchAndEmit(ctx context.Context, since time.Duration, limit int) {
	entries, err := s.client.QueryAgentEvents(ctx, s.session, limit, since)
	if err != nil {
		return // silently skip on error; VictoriaLogs may be unavailable
	}

	// VLogs responded — send a health signal so the TUI knows we're connected
	if !s.healthy {
		s.healthy = true
		select {
		case s.events <- Event{Type: "vlogs_healthy"}:
		default:
		}
	}

	// VictoriaLogs returns newest-first; sort oldest-first so the dedup
	// logic in addAgentEvent (which tracks lastSeenAgentTime) works correctly.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Time < entries[j].Time
	})

	for _, entry := range entries {
		event := agentEntryToEvent(entry)
		if event == nil {
			continue
		}
		// Only show mayor and polecat events — refinery/witness/deacon/boot
		// are infrastructure noise (use regular gt feed for those).
		if isIdleEvent(*event) {
			continue
		}
		select {
		case s.events <- *event:
		case <-ctx.Done():
			return
		default:
			// Drop if channel full
		}
	}
}

// agentEntryToEvent converts a VictoriaLogs log entry to a feed Event.
func agentEntryToEvent(entry vlogs.LogEntry) *Event {
	// Parse timestamp
	t, err := time.Parse(time.RFC3339Nano, entry.Time)
	if err != nil {
		t = time.Now()
	}

	// Extract agent identity from session ID (e.g., "gastown-mayor" → rig="gastown", actor="mayor")
	actor, rig, role := parseSessionID(entry.Session)

	// Generate human-readable summary based on event type
	var message string
	switch entry.EventType {
	case "tool_use":
		message = SummarizeToolUse(entry.Content)
	case "tool_result":
		// Skip tool_result events in the feed to reduce noise
		return nil
	case "text":
		message = SummarizeText(entry.Content)
		if message == "" {
			return nil
		}
	case "thinking":
		// Skip thinking events
		return nil
	case "usage":
		// Skip usage events in the main feed (shown in stats)
		return nil
	default:
		message = entry.EventType
	}

	// Distinguish user input from agent output
	eventRole := role
	if entry.Role == "user" && entry.EventType == "text" {
		eventRole = "human"
		actor = "mad-max"
	}

	return &Event{
		Time:    t,
		Type:    "agent_" + entry.EventType,
		Actor:   actor,
		Target:  entry.NativeSessionID,
		Message: message,
		Rig:     rig,
		Role:    eventRole,
		Raw:     entry.Content,
	}
}

// parseSessionID extracts actor, rig, and role from a Gas Town session ID.
// Examples:
//
//	"gastown-mayor"         → ("mayor", "gastown", "mayor")
//	"gastown-polecat-nux"   → ("gastown/nux", "gastown", "polecat")
//	"gastown-crew-joe"      → ("gastown/crew/joe", "gastown", "crew")
//	"gastown-witness"       → ("witness", "gastown", "witness")
func parseSessionID(session string) (actor, rig, role string) {
	if session == "" {
		return "unknown", "", ""
	}

	parts := strings.Split(session, "-")
	if len(parts) < 2 {
		return session, "", ""
	}

	rig = parts[0]
	role = parts[1]

	switch role {
	case "mayor", "deacon":
		actor = role
	case "witness", "refinery":
		actor = role
	case "crew":
		if len(parts) >= 3 {
			name := strings.Join(parts[2:], "-")
			actor = rig + "/crew/" + name
		} else {
			actor = role
		}
	case "polecat":
		if len(parts) >= 3 {
			name := strings.Join(parts[2:], "-")
			actor = rig + "/" + name
		} else {
			actor = role
		}
	default:
		// Could be a polecat name directly (e.g., "gastown-nux")
		actor = rig + "/" + role
		role = "polecat"
	}

	return
}

// Events returns the event channel.
func (s *AgentsSource) Events() <-chan Event {
	return s.events
}

// Close stops polling.
func (s *AgentsSource) Close() error {
	s.cancel()
	return nil
}
