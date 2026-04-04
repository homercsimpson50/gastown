package feed

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// toolCall represents a parsed Claude tool_use event.
type toolCall struct {
	Type  string                 `json:"type"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// SummarizeToolUse produces a concise 1-line description of a tool_use event.
// Handles three formats:
//  1. Wrapped: {"type":"tool_use","name":"Bash","input":{"command":"..."}}
//  2. Prefixed: Bash: {"command":"gt done","timeout":60000}
//  3. Raw JSON: {"command":"gt done","timeout":60000}
func SummarizeToolUse(content string) string {
	// Try wrapped format first
	var tc toolCall
	if err := json.Unmarshal([]byte(content), &tc); err == nil && tc.Name != "" {
		return summarizeTool(tc.Name, tc.Input)
	}

	// Try prefixed format: "ToolName: {json...}"
	if idx := strings.Index(content, ": {"); idx > 0 && idx < 30 {
		toolName := content[:idx]
		jsonPart := content[idx+2:]
		var input map[string]interface{}
		if err := json.Unmarshal([]byte(jsonPart), &input); err == nil {
			return summarizeTool(toolName, input)
		}
	}

	// Try raw input format — infer tool from fields present
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(content), &raw); err == nil {
		return summarizeRawInput(raw)
	}

	// Plain text content — just truncate
	return truncateMsg(content, 80)
}

// summarizeRawInput infers the tool type from the fields in the JSON content
// and generates a human-readable summary.
func summarizeRawInput(raw map[string]interface{}) string {
	// Bash: has "command" field
	if _, ok := raw["command"].(string); ok {
		return summarizeBash(raw)
	}
	// Read: has "file_path" and no "old_string"
	if _, ok := raw["file_path"]; ok {
		if _, hasOld := raw["old_string"]; hasOld {
			return summarizeEdit(raw)
		}
		if _, hasContent := raw["content"]; hasContent {
			return summarizeWrite(raw)
		}
		return summarizeRead(raw)
	}
	// Grep: has "pattern"
	if _, ok := raw["pattern"]; ok {
		return summarizeGrep(raw)
	}
	// Glob: has "pattern" but in glob context
	if _, ok := raw["glob"]; ok {
		return summarizeGlob(raw)
	}
	// Agent: has "description" or "prompt"
	if _, ok := raw["description"]; ok {
		if _, hasPrompt := raw["prompt"]; hasPrompt {
			return summarizeAgent(raw)
		}
	}

	// Unknown — show truncated JSON
	return truncateMsg(fmt.Sprintf("%v", raw), 60)
}

// SummarizeToolResult produces a concise 1-line description of a tool_result event.
func SummarizeToolResult(content string) string {
	if len(content) == 0 {
		return ""
	}
	// Tool results are often long; just show a brief prefix
	return truncateMsg(content, 60)
}

// SummarizeText produces a concise 1-line description of assistant text output.
func SummarizeText(content string) string {
	if len(content) == 0 {
		return ""
	}
	// First line of text, truncated
	line := strings.SplitN(content, "\n", 2)[0]
	return truncateMsg(line, 80)
}

func summarizeTool(name string, input map[string]interface{}) string {
	switch name {
	case "Read":
		return summarizeRead(input)
	case "Write":
		return summarizeWrite(input)
	case "Edit":
		return summarizeEdit(input)
	case "Bash":
		return summarizeBash(input)
	case "Grep":
		return summarizeGrep(input)
	case "Glob":
		return summarizeGlob(input)
	case "Agent":
		return summarizeAgent(input)
	case "WebSearch":
		return summarizeWebSearch(input)
	case "WebFetch":
		return summarizeWebFetch(input)
	case "Skill":
		return summarizeSkill(input)
	case "TaskCreate":
		return summarizeTaskCreate(input)
	case "TaskUpdate":
		return summarizeTaskUpdate(input)
	default:
		return fmt.Sprintf("Used %s", name)
	}
}

func summarizeRead(input map[string]interface{}) string {
	fp := getStr(input, "file_path")
	if fp == "" {
		return "Read file"
	}
	short := filepath.Base(fp)
	offset := getInt(input, "offset")
	limit := getInt(input, "limit")
	if offset > 0 || limit > 0 {
		if limit > 0 {
			return fmt.Sprintf("Read %s (lines %d-%d)", short, offset+1, offset+limit)
		}
		return fmt.Sprintf("Read %s (from line %d)", short, offset+1)
	}
	return fmt.Sprintf("Read %s", short)
}

func summarizeWrite(input map[string]interface{}) string {
	fp := getStr(input, "file_path")
	if fp == "" {
		return "Write file"
	}
	return fmt.Sprintf("Write %s", filepath.Base(fp))
}

func summarizeEdit(input map[string]interface{}) string {
	fp := getStr(input, "file_path")
	if fp == "" {
		return "Edit file"
	}
	return fmt.Sprintf("Edit %s", filepath.Base(fp))
}

func summarizeBash(input map[string]interface{}) string {
	cmd := getStr(input, "command")
	if cmd == "" {
		return "Run command"
	}
	// Truncate long commands
	cmd = strings.TrimSpace(cmd)
	if strings.Contains(cmd, "&&") {
		// Multiple chained commands — show first
		parts := strings.SplitN(cmd, "&&", 2)
		cmd = strings.TrimSpace(parts[0]) + " && ..."
	}
	return fmt.Sprintf("Run: %s", truncateMsg(cmd, 60))
}

func summarizeGrep(input map[string]interface{}) string {
	pattern := getStr(input, "pattern")
	if pattern == "" {
		return "Search codebase"
	}
	glob := getStr(input, "glob")
	if glob != "" {
		return fmt.Sprintf("Search '%s' in %s", truncateMsg(pattern, 30), glob)
	}
	path := getStr(input, "path")
	if path != "" {
		return fmt.Sprintf("Search '%s' in %s", truncateMsg(pattern, 30), filepath.Base(path))
	}
	return fmt.Sprintf("Search '%s'", truncateMsg(pattern, 40))
}

func summarizeGlob(input map[string]interface{}) string {
	pattern := getStr(input, "pattern")
	if pattern == "" {
		return "Find files"
	}
	return fmt.Sprintf("Find files: %s", pattern)
}

func summarizeAgent(input map[string]interface{}) string {
	desc := getStr(input, "description")
	if desc != "" {
		return fmt.Sprintf("Agent: %s", truncateMsg(desc, 50))
	}
	return "Spawned agent"
}

func summarizeWebSearch(input map[string]interface{}) string {
	query := getStr(input, "query")
	if query != "" {
		return fmt.Sprintf("Web search: %s", truncateMsg(query, 50))
	}
	return "Web search"
}

func summarizeWebFetch(input map[string]interface{}) string {
	u := getStr(input, "url")
	if u != "" {
		return fmt.Sprintf("Fetch: %s", truncateMsg(u, 50))
	}
	return "Web fetch"
}

func summarizeSkill(input map[string]interface{}) string {
	skill := getStr(input, "skill")
	if skill != "" {
		return fmt.Sprintf("/%s", skill)
	}
	return "Run skill"
}

func summarizeTaskCreate(input map[string]interface{}) string {
	desc := getStr(input, "description")
	if desc != "" {
		return fmt.Sprintf("Create task: %s", truncateMsg(desc, 50))
	}
	return "Create task"
}

func summarizeTaskUpdate(input map[string]interface{}) string {
	status := getStr(input, "status")
	if status != "" {
		return fmt.Sprintf("Task → %s", status)
	}
	return "Update task"
}

// getStr extracts a string value from a map.
func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getInt extracts an int value from a map.
func getInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// truncateMsg truncates a string to max runes and appends "..." if trimmed.
func truncateMsg(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-3]) + "..."
}
