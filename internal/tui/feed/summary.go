package feed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultOllamaURL is the default Ollama API endpoint.
	DefaultOllamaURL = "http://localhost:11434"

	// EnvOllamaURL overrides the Ollama endpoint.
	EnvOllamaURL = "OLLAMA_URL"

	// EnvOllamaModel overrides the model name.
	EnvOllamaModel = "OLLAMA_MODEL"

	// DefaultModel is the default Ollama model for summarization.
	DefaultModel = "qwen2.5:1.5b"

	// SummaryInterval is how often to refresh the summary.
	SummaryInterval = 10 * time.Second

	// SummaryWindow is how many seconds of events to include.
	SummaryWindow = 30 * time.Second
)

// ollamaRequest is the Ollama API generate request.
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// ollamaResponse is the Ollama API generate response.
type ollamaResponse struct {
	Response      string `json:"response"`
	TotalDuration int64  `json:"total_duration"`
	EvalCount     int    `json:"eval_count"`
}

// SummaryProvider generates AI summaries of agent events using a local LLM.
type SummaryProvider struct {
	url       string
	model     string
	client    *http.Client
	available bool

	mu            sync.RWMutex
	lastSummary   string
	lastUpdated   time.Time
	lastDuration  time.Duration
	summarizing   bool
}

// NewSummaryProvider creates a new summary provider.
// Checks if Ollama is reachable on creation.
func NewSummaryProvider() *SummaryProvider {
	url := os.Getenv(EnvOllamaURL)
	if url == "" {
		url = DefaultOllamaURL
	}
	model := os.Getenv(EnvOllamaModel)
	if model == "" {
		model = DefaultModel
	}

	sp := &SummaryProvider{
		url:   url,
		model: model,
		client: &http.Client{
			Timeout: 120 * time.Second, // LLM inference can be slow
		},
	}

	// Check availability in background (don't block startup)
	go sp.checkAvailable()

	return sp
}

// checkAvailable pings Ollama to see if it's running.
func (sp *SummaryProvider) checkAvailable() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", sp.url+"/api/tags", nil)
	if err != nil {
		return
	}

	resp, err := sp.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()

	sp.mu.Lock()
	sp.available = resp.StatusCode == http.StatusOK
	sp.mu.Unlock()
}

// Available returns whether Ollama is reachable.
func (sp *SummaryProvider) Available() bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.available
}

// Summary returns the latest summary text and when it was generated.
func (sp *SummaryProvider) Summary() (text string, age time.Duration, inferDuration time.Duration) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	if sp.lastSummary == "" {
		return "", 0, 0
	}
	return sp.lastSummary, time.Since(sp.lastUpdated), sp.lastDuration
}

// IsSummarizing returns whether a summary is currently being generated.
func (sp *SummaryProvider) IsSummarizing() bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.summarizing
}

// Summarize generates a summary from recent events. Non-blocking — runs in background.
// Returns immediately. Use Summary() to read the result.
func (sp *SummaryProvider) Summarize(events []Event) {
	sp.mu.Lock()
	if sp.summarizing || !sp.available {
		sp.mu.Unlock()
		return
	}
	sp.summarizing = true
	sp.mu.Unlock()

	go func() {
		defer func() {
			sp.mu.Lock()
			sp.summarizing = false
			sp.mu.Unlock()
		}()

		summary, duration, err := sp.generate(events)
		if err != nil {
			sp.mu.Lock()
			sp.lastSummary = fmt.Sprintf("(summary error: %v)", err)
			sp.lastUpdated = time.Now()
			sp.mu.Unlock()
			return
		}

		sp.mu.Lock()
		sp.lastSummary = summary
		sp.lastUpdated = time.Now()
		sp.lastDuration = duration
		sp.mu.Unlock()
	}()
}

// generate calls Ollama API synchronously.
func (sp *SummaryProvider) generate(events []Event) (string, time.Duration, error) {
	if len(events) == 0 {
		return "No recent agent activity.", 0, nil
	}

	// Build event text
	var lines []string
	for _, e := range events {
		actor := e.Actor
		if parts := strings.Split(actor, "/"); len(parts) > 0 {
			actor = parts[len(parts)-1]
		}
		line := fmt.Sprintf("%s %s/%s: %s",
			e.Time.Format("15:04:05"),
			e.Rig, actor,
			e.Message)
		lines = append(lines, line)
	}

	prompt := fmt.Sprintf(`You are an AI agent activity summarizer. Given these recent agent tool-call events from a software development system, write a 2-3 sentence summary of what is happening right now. Be concise and specific. Focus on what work is being done, by whom, and the current status. Do not repeat the events — synthesize them.

Events (most recent first):
%s

Summary:`, strings.Join(lines, "\n"))

	reqBody := ollamaRequest{
		Model:  sp.model,
		Prompt: prompt,
		Stream: false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, err
	}

	req, err := http.NewRequest("POST", sp.url+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := sp.client.Do(req)
	if err != nil {
		// Mark as unavailable so we don't keep trying
		sp.mu.Lock()
		sp.available = false
		sp.mu.Unlock()
		return "", 0, fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", 0, err
	}

	duration := time.Duration(ollamaResp.TotalDuration)
	return strings.TrimSpace(ollamaResp.Response), duration, nil
}
