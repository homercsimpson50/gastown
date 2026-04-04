package feed

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// render produces the full TUI output
func (m *Model) render() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	var sections []string

	// Header
	sections = append(sections, m.renderHeader())

	if m.viewMode == ViewAgents {
		if m.showSummary {
			// Split-screen: events left, summary right
			sections = append(sections, m.renderAgentsSplitView())
		} else {
			// Full-width events
			agentsPanel := m.renderAgentsPanel()
			sections = append(sections, agentsPanel)
		}
	} else if m.viewMode == ViewProblems {
		// Problems view: single panel
		problemsPanel := m.renderProblemsPanel()
		sections = append(sections, problemsPanel)
	} else {
		// Activity view: three panels
		// Tree panel (top)
		treePanel := m.renderTreePanel()
		sections = append(sections, treePanel)

		// Convoy panel (middle)
		convoyPanel := m.renderConvoyPanel()
		sections = append(sections, convoyPanel)

		// Feed panel (bottom)
		feedPanel := m.renderFeedPanel()
		sections = append(sections, feedPanel)
	}

	// Status bar
	sections = append(sections, m.renderStatusBar())

	// Help (if shown)
	if m.showHelp {
		sections = append(sections, m.help.View(m.keys))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderHeader renders the top header bar
func (m *Model) renderHeader() string {
	var title string
	switch m.viewMode {
	case ViewAgents:
		title = TitleStyle.Render("GT Feed") + " " + AgentsModeStyle.Render("[AGENTS]")
	case ViewProblems:
		title = TitleStyle.Render("GT Feed") + " " + ProblemsModeStyle.Render("[PROBLEMS]")
	default:
		title = TitleStyle.Render("GT Feed")
	}

	// Show summary stats on the right
	var stats string
	if m.viewMode == ViewAgents {
		healthy := ""
		if m.agentsHealthy {
			healthy = AgentActiveStyle.Render("●") + " VictoriaLogs"
		} else {
			healthy = EventFailStyle.Render("●") + " VictoriaLogs"
		}
		stats = fmt.Sprintf("%d events  %s", len(m.agentEvents), healthy)
	} else if m.viewMode == ViewProblems && len(m.problemAgents) > 0 {
		ok, stuck, idle := m.countAgentStates()
		stats = fmt.Sprintf("%d agents  %s %d ok │ %s %d stuck │ %d idle",
			len(m.problemAgents),
			AgentActiveStyle.Render("●"), ok,
			EventFailStyle.Render("●"), stuck,
			idle)
	} else if m.filter != "" {
		stats = FilterStyle.Render(fmt.Sprintf("Filter: %s", m.filter))
	} else {
		stats = FilterStyle.Render("Filter: all")
	}

	// Right-align stats
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(stats) - 4
	if gap < 1 {
		gap = 1
	}

	return HeaderStyle.Render(title + strings.Repeat(" ", gap) + stats)
}

// countAgentStates returns counts of ok, stuck, and idle agents
func (m *Model) countAgentStates() (ok, stuck, idle int) {
	for _, agent := range m.problemAgents {
		switch agent.State {
		case StateWorking:
			ok++
		case StateIdle:
			idle++
		case StateGUPPViolation, StateStalled, StateZombie:
			stuck++
		}
	}
	return
}

// renderTreePanel renders the agent tree panel with border
func (m *Model) renderTreePanel() string {
	style := TreePanelStyle
	if m.focusedPanel == PanelTree {
		style = FocusedBorderStyle
	}
	return style.Width(m.width - 2).Render(m.treeViewport.View())
}

// renderFeedPanel renders the event feed panel with border
func (m *Model) renderFeedPanel() string {
	style := StreamPanelStyle
	if m.focusedPanel == PanelFeed {
		style = FocusedBorderStyle
	}
	return style.Width(m.width - 2).Render(m.feedViewport.View())
}

// renderProblemsPanel renders the problems view panel
func (m *Model) renderProblemsPanel() string {
	style := ProblemsPanelStyle
	if m.focusedPanel == PanelProblems {
		style = FocusedBorderStyle
	}
	return style.Width(m.width - 2).Render(m.problemsViewport.View())
}

// renderProblemsContent renders the problems view content
func (m *Model) renderProblemsContent() string {
	var lines []string

	if m.problemsError != nil {
		return AgentIdleStyle.Render(fmt.Sprintf("Error fetching agent status: %v\nRetrying...", m.problemsError))
	}

	if len(m.problemAgents) == 0 {
		return AgentIdleStyle.Render("No agents detected. Run gt feed in a GasTown workspace with active agents.")
	}

	// Count problems
	var problemAgents []*ProblemAgent
	var workingAgents []*ProblemAgent
	var idleAgents []*ProblemAgent

	for _, agent := range m.problemAgents {
		switch {
		case agent.State.NeedsAttention():
			problemAgents = append(problemAgents, agent)
		case agent.State == StateWorking:
			workingAgents = append(workingAgents, agent)
		default:
			idleAgents = append(idleAgents, agent)
		}
	}

	// NEEDS ATTENTION section
	if len(problemAgents) > 0 {
		lines = append(lines, ProblemsHeaderStyle.Render(fmt.Sprintf("NEEDS ATTENTION (%d)", len(problemAgents))))
		lines = append(lines, "")
		for i, agent := range problemAgents {
			isSelected := i == m.selectedProblem
			lines = append(lines, m.renderProblemAgent(agent, isSelected))
		}
		lines = append(lines, "")
	} else {
		lines = append(lines, ProblemsHeaderStyle.Render("NEEDS ATTENTION (0)"))
		lines = append(lines, "  "+AgentActiveStyle.Render("All agents OK!"))
		lines = append(lines, "")
	}

	// WORKING section (collapsed dots by rig)
	if len(workingAgents) > 0 {
		lines = append(lines, WorkingHeaderStyle.Render(fmt.Sprintf("WORKING (%d)", len(workingAgents))))
		// Group by rig
		byRig := make(map[string]int)
		for _, agent := range workingAgents {
			rig := agent.Rig
			if rig == "" {
				rig = "default"
			}
			byRig[rig]++
		}
		for rig, count := range byRig {
			dots := strings.Repeat("●", count)
			if count > 20 {
				dots = strings.Repeat("●", 20) + fmt.Sprintf("+%d", count-20)
			}
			lines = append(lines, fmt.Sprintf("  %s %s (%d)",
				AgentActiveStyle.Render(dots),
				RigStyle.Render(rig),
				count))
		}
		lines = append(lines, "")
	}

	// IDLE section (collapsed)
	if len(idleAgents) > 0 {
		lines = append(lines, IdleHeaderStyle.Render(fmt.Sprintf("IDLE (%d)", len(idleAgents))))
		dots := strings.Repeat("○", len(idleAgents))
		if len(idleAgents) > 20 {
			dots = strings.Repeat("○", 20) + fmt.Sprintf("+%d", len(idleAgents)-20)
		}
		lines = append(lines, "  "+AgentIdleStyle.Render(dots))
	}

	return strings.Join(lines, "\n")
}

// renderProblemAgent renders a single problem agent line
func (m *Model) renderProblemAgent(agent *ProblemAgent, selected bool) string {
	// Format: "▶polecat-12  🔥 GUPP!    45m (violation)  gt-xyz89   myproject"
	prefix := "  "
	if selected {
		prefix = SelectedStyle.Render("▶ ")
	}

	// Name
	name := agent.Name
	if len(name) > 12 {
		name = name[:12]
	}
	namePart := fmt.Sprintf("%-12s", name)

	// State symbol and label
	stateStyle := getStateStyle(agent.State)
	statePart := stateStyle.Render(fmt.Sprintf("%s %-6s", agent.State.Symbol(), agent.State.Label()))

	// Duration
	reasonPart := fmt.Sprintf("%-20s", fmt.Sprintf("%s no progress", agent.DurationDisplay()))

	// Bead ID (if known)
	beadPart := ""
	if agent.CurrentBeadID != "" {
		beadPart = ConvoyIDStyle.Render(agent.CurrentBeadID)
	}

	// Rig
	rigPart := ""
	if agent.Rig != "" {
		rigPart = RigStyle.Render(agent.Rig)
	}

	return prefix + namePart + "  " + statePart + "  " + TimestampStyle.Render(reasonPart) + "  " + beadPart + "  " + rigPart
}

// getStateStyle returns the appropriate style for an agent state
func getStateStyle(state AgentState) lipgloss.Style {
	switch state {
	case StateGUPPViolation:
		return GUPPStyle
	case StateStalled:
		return StalledStyle
	case StateZombie:
		return ZombieStyle
	default:
		return AgentIdleStyle
	}
}

// renderTree renders the agent tree content.
// Caller must hold m.mu.
func (m *Model) renderTree() string {
	if len(m.rigs) == 0 {
		return AgentIdleStyle.Render("No agents active")
	}

	var lines []string

	// Sort rigs by name
	rigNames := make([]string, 0, len(m.rigs))
	for name := range m.rigs {
		rigNames = append(rigNames, name)
	}
	sort.Strings(rigNames)

	for _, rigName := range rigNames {
		rig := m.rigs[rigName]

		// Rig header
		rigLine := RigStyle.Render(rigName + "/")
		lines = append(lines, rigLine)

		// Group agents by role
		byRole := m.groupAgentsByRole(rig.Agents)

		// Render each role group
		roleOrder := []string{"mayor", "witness", "refinery", "deacon", "crew", "polecat"}
		for _, role := range roleOrder {
			agents, ok := byRole[role]
			if !ok || len(agents) == 0 {
				continue
			}

			icon := RoleIcons[role]
			if icon == "" {
				icon = "•"
			}

			// For crew and polecats, show as expandable group
			if role == "crew" || role == "polecat" {
				lines = append(lines, m.renderAgentGroup(icon, role, agents))
			} else {
				// Single agents (mayor, witness, refinery)
				for _, agent := range agents {
					lines = append(lines, m.renderAgent(icon, agent, 2))
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

// groupAgentsByRole groups agents by their role
func (m *Model) groupAgentsByRole(agents map[string]*Agent) map[string][]*Agent {
	result := make(map[string][]*Agent)
	for _, agent := range agents {
		role := agent.Role
		if role == "" {
			role = "unknown"
		}
		result[role] = append(result[role], agent)
	}

	// Sort each group by name
	for role := range result {
		sort.Slice(result[role], func(i, j int) bool {
			return result[role][i].Name < result[role][j].Name
		})
	}

	return result
}

// renderAgentGroup renders a group of agents (crew or polecats)
func (m *Model) renderAgentGroup(icon, role string, agents []*Agent) string {
	var lines []string

	// Group header
	plural := role
	if role == "polecat" {
		plural = "polecats"
	}
	header := fmt.Sprintf("  %s %s/", icon, plural)
	lines = append(lines, RoleStyle.Render(header))

	// Individual agents
	for _, agent := range agents {
		lines = append(lines, m.renderAgent("", agent, 5))
	}

	return strings.Join(lines, "\n")
}

// renderAgent renders a single agent line
func (m *Model) renderAgent(icon string, agent *Agent, indent int) string {
	prefix := strings.Repeat(" ", indent)
	if icon != "" && indent >= 2 {
		prefix = strings.Repeat(" ", indent-2) + icon + " "
	} else if icon != "" {
		prefix = icon + " "
	}

	// Name with status indicator
	name := agent.Name
	// Extract just the short name if it's a full path
	if parts := strings.Split(name, "/"); len(parts) > 0 {
		name = parts[len(parts)-1]
	}

	nameStyle := AgentIdleStyle
	statusIndicator := ""
	if agent.Status == "running" || agent.Status == "working" {
		nameStyle = AgentActiveStyle
		statusIndicator = " →"
	}

	// Last activity
	activity := ""
	if agent.LastEvent != nil {
		age := formatAge(time.Since(agent.LastEvent.Time))
		msg := agent.LastEvent.Message
		if len(msg) > 40 {
			msg = msg[:37] + "..."
		}
		activity = fmt.Sprintf(" [%s] %s", age, msg)
	}

	line := prefix + nameStyle.Render(name+statusIndicator) + TimestampStyle.Render(activity)
	return line
}

// renderFeed renders the event feed content.
// Caller must hold m.mu.
func (m *Model) renderFeed() string {
	if len(m.events) == 0 {
		return AgentIdleStyle.Render("No events yet")
	}

	var lines []string

	// Show most recent events first (reversed)
	start := 0
	if len(m.events) > 100 {
		start = len(m.events) - 100
	}

	for i := len(m.events) - 1; i >= start; i-- {
		event := m.events[i]
		lines = append(lines, m.renderEvent(event))
	}

	return strings.Join(lines, "\n")
}

// renderEvent renders a single event line
func (m *Model) renderEvent(e Event) string {
	// Timestamp - compact HH:MM format, no brackets
	ts := TimestampStyle.Render(e.Time.Local().Format("15:04"))

	// Symbol based on event type
	symbol := EventSymbols[e.Type]
	if symbol == "" {
		symbol = "•"
	}

	// Style based on event type
	var symbolStyle lipgloss.Style
	switch e.Type {
	case "create":
		symbolStyle = EventCreateStyle
	case "update":
		symbolStyle = EventUpdateStyle
	case "complete", "patrol_complete", "merged", "done":
		symbolStyle = EventCompleteStyle
	case "fail", "merge_failed":
		symbolStyle = EventFailStyle
	case "delete":
		symbolStyle = EventDeleteStyle
	case "merge_started":
		symbolStyle = EventMergeStartedStyle
	case "merge_skipped":
		symbolStyle = EventMergeSkippedStyle
	case "patrol_started", "polecat_checked":
		symbolStyle = EventUpdateStyle
	case "polecat_nudged", "escalation_sent", "nudge":
		symbolStyle = EventFailStyle // Use red/warning style for nudges and escalations
	case "sling", "hook", "spawn", "boot":
		symbolStyle = EventCreateStyle
	case "handoff", "mail":
		symbolStyle = EventUpdateStyle
	default:
		symbolStyle = EventUpdateStyle
	}

	styledSymbol := symbolStyle.Render(symbol)

	// Actor (short form)
	actor := ""
	if e.Actor != "" {
		parts := strings.Split(e.Actor, "/")
		if len(parts) > 0 {
			actor = parts[len(parts)-1]
		}
		if icon := RoleIcons[e.Role]; icon != "" {
			actor = icon + " " + actor
		}
		actor = RoleStyle.Render(actor) + ": "
	}

	// Message
	msg := e.Message
	if msg == "" && e.Raw != "" {
		msg = e.Raw
	}

	return fmt.Sprintf("%s %s %s%s", ts, styledSymbol, actor, msg)
}

// renderStatusBar renders the bottom status bar.
func (m *Model) renderStatusBar() string {
	var left string
	if m.viewMode == ViewAgents {
		left = fmt.Sprintf("[agents] %d tool calls", len(m.agentEvents))
		if m.agentRigFilter != "" {
			left += fmt.Sprintf(" | rig: %s", m.agentRigFilter)
		}
		if m.agentSessionFilter != "" {
			left += fmt.Sprintf(" | session: %s", m.agentSessionFilter)
		}
	} else if m.viewMode == ViewProblems {
		// Problems view: show problem count and selected agent
		problemCount := 0
		for _, agent := range m.problemAgents {
			if agent.State.NeedsAttention() {
				problemCount++
			}
		}
		left = fmt.Sprintf("[problems] %d need attention", problemCount)
		if selected := m.getSelectedProblemAgent(); selected != nil {
			left += fmt.Sprintf(" | selected: %s", selected.Name)
		}
	} else {
		// Activity view: show panel and event count
		var panelName string
		switch m.focusedPanel {
		case PanelTree:
			panelName = "tree"
		case PanelConvoy:
			panelName = "convoy"
		case PanelFeed:
			panelName = "feed"
		}
		left = fmt.Sprintf("[%s] %d events", panelName, len(m.events))
	}

	// Short help
	help := m.renderShortHelp()

	// Combine
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(help) - 4
	if gap < 1 {
		gap = 1
	}

	return StatusBarStyle.Width(m.width).Render(left + strings.Repeat(" ", gap) + help)
}

// renderShortHelp renders abbreviated key hints
func (m *Model) renderShortHelp() string {
	if m.viewMode == ViewAgents {
		summaryHint := HelpKeyStyle.Render("s") + HelpDescStyle.Render(":summary")
		if m.showSummary {
			summaryHint = HelpKeyStyle.Render("s") + HelpDescStyle.Render(":hide summary")
		}
		hints := []string{
			HelpKeyStyle.Render("a") + HelpDescStyle.Render(":activity"),
			HelpKeyStyle.Render("r") + HelpDescStyle.Render(":rig"),
			summaryHint,
			HelpKeyStyle.Render("j/k") + HelpDescStyle.Render(":scroll"),
			HelpKeyStyle.Render("R") + HelpDescStyle.Render(":refresh"),
			HelpKeyStyle.Render("q") + HelpDescStyle.Render(":quit"),
		}
		return strings.Join(hints, "  ")
	}
	if m.viewMode == ViewProblems {
		hints := []string{
			HelpKeyStyle.Render("p") + HelpDescStyle.Render(":activity"),
			HelpKeyStyle.Render("⏎") + HelpDescStyle.Render(":attach"),
			HelpKeyStyle.Render("n") + HelpDescStyle.Render(":nudge"),
			HelpKeyStyle.Render("h") + HelpDescStyle.Render(":handoff"),
			HelpKeyStyle.Render("Tab") + HelpDescStyle.Render(":next"),
			HelpKeyStyle.Render("?") + HelpDescStyle.Render(":help"),
			HelpKeyStyle.Render("q") + HelpDescStyle.Render(":quit"),
		}
		return strings.Join(hints, "  ")
	}
	hints := []string{
		HelpKeyStyle.Render("a") + HelpDescStyle.Render(":agents"),
		HelpKeyStyle.Render("p") + HelpDescStyle.Render(":problems"),
		HelpKeyStyle.Render("j/k") + HelpDescStyle.Render(":scroll"),
		HelpKeyStyle.Render("tab") + HelpDescStyle.Render(":switch"),
		HelpKeyStyle.Render("/") + HelpDescStyle.Render(":search"),
		HelpKeyStyle.Render("q") + HelpDescStyle.Render(":quit"),
		HelpKeyStyle.Render("?") + HelpDescStyle.Render(":help"),
	}
	return strings.Join(hints, "  ")
}

// renderAgentsPanel renders the agents observability panel with border
func (m *Model) renderAgentsPanel() string {
	style := AgentsPanelStyle
	if m.focusedPanel == PanelAgents {
		style = FocusedBorderStyle
	}
	return style.Width(m.width - 2).Render(m.agentsViewport.View())
}

// renderAgentsFeed renders the agent tool-call feed content.
// Caller must hold m.mu.
func (m *Model) renderAgentsFeed() string {
	if len(m.agentEvents) == 0 {
		if !m.agentsHealthy {
			return AgentIdleStyle.Render("Connecting to VictoriaLogs...\n\n" +
				"Ensure VictoriaLogs is running and GT_VLOGS_QUERY_URL is set.\n" +
				"Default: http://localhost:9428/select/logsql/query")
		}
		return AgentIdleStyle.Render("No agent events yet. Waiting for tool calls...")
	}

	var lines []string

	// Show events in reverse chronological order (newest first), filtered by rig
	count := 0
	for i := len(m.agentEvents) - 1; i >= 0 && count < 200; i-- {
		e := m.agentEvents[i]
		if m.agentRigFilter != "" && e.Rig != m.agentRigFilter {
			continue
		}
		lines = append(lines, m.renderAgentEvent(e))
		count++
	}

	if len(lines) == 0 {
		return AgentIdleStyle.Render(fmt.Sprintf("No events for rig '%s'. Press r to cycle filter.", m.agentRigFilter))
	}

	return strings.Join(lines, "\n")
}

// renderAgentEvent renders a single agent tool-call event line.
// Format: "03:42:01  gastown  😺 mayor    Read CHRONICLE.md (lines 1-50)"
func (m *Model) renderAgentEvent(e Event) string {
	// Timestamp
	ts := TimestampStyle.Render(e.Time.Local().Format("15:04:05"))

	// Rig (right-padded)
	rig := e.Rig
	if rig == "" {
		rig = "—"
	}
	if len(rig) > 12 {
		rig = rig[:12]
	}
	rigStr := RigStyle.Render(fmt.Sprintf("%-12s", rig))

	// Actor (short form, right-padded)
	actor := "unknown"
	if e.Actor != "" {
		parts := strings.Split(e.Actor, "/")
		actor = parts[len(parts)-1]
	}
	if len(actor) > 10 {
		actor = actor[:10]
	}
	actorStr := fmt.Sprintf("%-10s", actor)

	// Role icon
	icon := RoleIcons[e.Role]
	if icon == "" {
		icon = "•"
	}

	// Message (already summarized by the source)
	msg := e.Message

	return fmt.Sprintf("%s  %s %s %s %s", ts, rigStr, icon, RoleStyle.Render(actorStr), msg)
}

// renderAgentsSplitView renders the agents view with a summary panel on the right.
func (m *Model) renderAgentsSplitView() string {
	// Split width: 65% events, 35% summary
	totalWidth := m.width - 4
	eventsWidth := totalWidth * 65 / 100
	summaryWidth := totalWidth - eventsWidth - 1 // 1 for divider

	if eventsWidth < 40 {
		eventsWidth = 40
	}
	if summaryWidth < 20 {
		summaryWidth = 20
	}

	// Calculate available height for content
	panelHeight := m.agentsViewport.Height
	if panelHeight < 5 {
		panelHeight = 10
	}

	// Left panel: events
	eventsContent := m.renderAgentsFeed()
	eventsStyle := lipgloss.NewStyle().
		Width(eventsWidth).
		MaxWidth(eventsWidth).
		Height(panelHeight).
		MaxHeight(panelHeight)

	// Grey divider line
	dividerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Height(panelHeight).
		MaxHeight(panelHeight)
	divider := strings.Repeat("│\n", panelHeight)
	if len(divider) > 0 {
		divider = divider[:len(divider)-1] // trim trailing newline
	}

	// Right panel: summary (uses viewport for scrolling)
	summaryContent := m.renderSummaryContent(summaryWidth - 2)
	m.summaryViewport.Width = summaryWidth - 2
	m.summaryViewport.Height = panelHeight
	m.summaryViewport.SetContent(summaryContent)

	summaryBorder := lipgloss.Color("240")
	if m.focusedPanel == PanelSummary {
		summaryBorder = lipgloss.Color("63")
	}
	summaryStyle := lipgloss.NewStyle().
		Width(summaryWidth).
		MaxWidth(summaryWidth).
		Height(panelHeight).
		MaxHeight(panelHeight).
		PaddingLeft(1).
		BorderForeground(summaryBorder)

	left := eventsStyle.Render(eventsContent)
	mid := dividerStyle.Render(divider)
	right := summaryStyle.Render(m.summaryViewport.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, mid, right)
}

// renderSummaryContent renders the AI summary panel content.
func (m *Model) renderSummaryContent(width int) string {
	var lines []string

	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Render("AI Summary")
	lines = append(lines, header)
	lines = append(lines, "")

	if !m.summaryProvider.Available() {
		lines = append(lines, AgentIdleStyle.Render("Ollama not running."))
		lines = append(lines, "")
		lines = append(lines, AgentIdleStyle.Render("Start with:"))
		lines = append(lines, AgentIdleStyle.Render("  brew services start ollama"))
		return strings.Join(lines, "\n")
	}

	if m.summaryProvider.IsSummarizing() {
		lines = append(lines, AgentIdleStyle.Render("Summarizing..."))
		lines = append(lines, "")
	}

	text, age, dur := m.summaryProvider.Summary()
	if text == "" {
		lines = append(lines, AgentIdleStyle.Render("Waiting for events..."))
	} else {
		// Word-wrap the summary to fit the panel width
		wrapped := wordWrap(text, width)
		lines = append(lines, wrapped)
		lines = append(lines, "")

		// Stats line
		ageStr := formatAge(age)
		durStr := fmt.Sprintf("%.1fs", dur.Seconds())
		stats := TimestampStyle.Render(fmt.Sprintf("Updated %s ago (%s)", ageStr, durStr))
		lines = append(lines, stats)
	}

	return strings.Join(lines, "\n")
}

// wordWrap wraps text to the given width.
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}

	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = w
		} else {
			line += " " + w
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}

// formatAge formats a duration as a short age string
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
