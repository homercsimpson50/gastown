// Package vlogs provides an HTTP client for querying VictoriaLogs via LogsQL.
package vlogs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	// DefaultQueryURL is the VictoriaLogs LogsQL select endpoint.
	DefaultQueryURL = "http://localhost:9428/select/logsql/query"

	// EnvQueryURL overrides the default query URL.
	EnvQueryURL = "GT_VLOGS_QUERY_URL"
)

// LogEntry represents a single log record returned by VictoriaLogs.
// Fields are dynamic; common ones are extracted as top-level fields.
type LogEntry struct {
	Msg              string `json:"_msg"`
	Time             string `json:"_time"`
	Stream           string `json:"_stream"`
	Session          string `json:"session"`
	AgentType        string `json:"agent_type"`
	EventType        string `json:"event_type"`
	Role             string `json:"role"`
	Content          string `json:"content"`
	NativeSessionID  string `json:"native_session_id"`
	RunID            string `json:"run.id"`
	InputTokens      string `json:"input_tokens"`
	OutputTokens     string `json:"output_tokens"`
	CacheReadTokens  string `json:"cache_read_tokens"`
	CacheCreateTokens string `json:"cache_creation_tokens"`
}

// Client queries VictoriaLogs over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a VictoriaLogs client.
// Uses GT_VLOGS_QUERY_URL env var if set, otherwise DefaultQueryURL.
func New() *Client {
	baseURL := os.Getenv(EnvQueryURL)
	if baseURL == "" {
		baseURL = DefaultQueryURL
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Query executes a LogsQL query and returns matching log entries.
// The query is a LogsQL expression like: _msg:"agent.event" AND session:"gastown-mayor"
func (c *Client) Query(ctx context.Context, query string, limit int, start, end time.Time) ([]LogEntry, error) {
	params := url.Values{}
	params.Set("query", query)
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	if !start.IsZero() {
		params.Set("start", start.Format(time.RFC3339))
	}
	if !end.IsZero() {
		params.Set("end", end.Format(time.RFC3339))
	}

	reqURL := c.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying VictoriaLogs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("VictoriaLogs returned status %d", resp.StatusCode)
	}

	// VictoriaLogs returns streaming JSONL (one JSON object per line)
	var entries []LogEntry
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, entry)
	}

	return entries, scanner.Err()
}

// QueryAgentEvents queries for agent.event log records, optionally filtered
// by session ID. Returns events ordered by time (newest first from VictoriaLogs).
func (c *Client) QueryAgentEvents(ctx context.Context, session string, limit int, since time.Duration) ([]LogEntry, error) {
	q := `_msg:"agent.event"`
	if session != "" {
		q += fmt.Sprintf(` AND session:"%s"`, session)
	}

	start := time.Time{}
	if since > 0 {
		start = time.Now().Add(-since)
	}

	return c.Query(ctx, q, limit, start, time.Time{})
}

// QueryAgentUsage queries for agent.usage (token consumption) records.
func (c *Client) QueryAgentUsage(ctx context.Context, session string, limit int, since time.Duration) ([]LogEntry, error) {
	q := `_msg:"agent.usage"`
	if session != "" {
		q += fmt.Sprintf(` AND session:"%s"`, session)
	}

	start := time.Time{}
	if since > 0 {
		start = time.Now().Add(-since)
	}

	return c.Query(ctx, q, limit, start, time.Time{})
}

// Healthy checks if VictoriaLogs is reachable.
func (c *Client) Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Query with a 1-entry limit to test connectivity
	_, err := c.Query(ctx, "*", 1, time.Now().Add(-time.Minute), time.Time{})
	return err == nil
}
