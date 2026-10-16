package agent

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/llmrec"
	actool "github.com/Autumn-27/norma/tool"
	"github.com/Autumn-27/norma/transcript"
)

// DeepSeekSearchMeter captures the global search profile, which may differ from
// the task's inference profile. Usage is written even when body recording is off.
type DeepSeekSearchMeter struct {
	Store       *db.DB
	ProfileName string
}

type deepSeekSearch struct {
	actool.CoreTool // Preserve Norma's schema, metadata and permission decision.
	cfg             actool.WebSearchConfig
	client          *http.Client
	clientErr       error
	meter           DeepSeekSearchMeter
	drainTimeout    time.Duration
}

// NewDeepSeekWebSearch replaces only Call; Norma remains the permission authority.
func NewDeepSeekWebSearch(cfg actool.WebSearchConfig, meter DeepSeekSearchMeter) (actool.CoreTool, error) {
	original, err := actool.NewWebSearch(cfg)
	if err != nil {
		return nil, err
	}
	client, clientErr := deepSeekSearchClient(cfg)
	return &deepSeekSearch{CoreTool: original, cfg: cfg, client: client, clientErr: clientErr, meter: meter, drainTimeout: 10 * time.Second}, nil
}

// deepSeekSearchClientErr reports a fail-closed configuration error without
// building the client (used by tests to lock in proxy/CA/InsecureTLS precedence).
func deepSeekSearchClientErr(cfg actool.WebSearchConfig) error {
	_, err := deepSeekSearchClient(cfg)
	return err
}

func deepSeekSearchClient(cfg actool.WebSearchConfig) (*http.Client, error) {
	// Preserve direct/proxy/TLS behavior, but fail closed on invalid configuration.
	if cfg.Proxy == "" && !cfg.InsecureTLS && cfg.CACert == "" {
		return &http.Client{}, nil
	}
	tr := &http.Transport{}
	if cfg.Proxy != "" {
		u, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid search proxy URL")
		}
		if u.Hostname() == "" {
			return nil, fmt.Errorf("search proxy requires a host")
		}
		switch u.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, fmt.Errorf("unsupported search proxy scheme %q", u.Scheme)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	switch {
	case cfg.InsecureTLS:
		// InsecureTLS wins and ignores CACert, matching Norma's documented precedence.
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	case cfg.CACert != "":
		pem, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("could not load search CA: %w", err)
		}
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("could not parse search CA certificates")
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	}
	return &http.Client{Transport: tr}, nil
}

func (s *deepSeekSearch) Call(ctx context.Context, input json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return actool.Result{}, err
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return actool.Errorf("Error: query must not be empty"), nil
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	results, err := s.search(ctx, query, limit)
	if err != nil {
		return actool.Errorf(fmt.Sprintf("Error searching web via deepseek: %s", err)), nil
	}
	out, _ := json.MarshalIndent(map[string]any{"backend": "deepseek", "query": query, "results": results}, "", "  ")
	return actool.Text(deepSeekTruncate(string(out), 50000)), nil
}

// DeepSeekWebSearchProbe bypasses tool permissions, as the settings test always
// has, and shares the exact request/parser/metering path with the live tool.
func DeepSeekWebSearchProbe(ctx context.Context, cfg actool.WebSearchConfig, meter DeepSeekSearchMeter, query string, limit int) ([]actool.SearchResult, error) {
	tool, err := NewDeepSeekWebSearch(cfg, meter)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 3
	}
	return tool.(*deepSeekSearch).search(ctx, query, limit)
}

type deepSeekUsage struct {
	Input   int `json:"input_tokens"`
	Output  int `json:"output_tokens"`
	Read    int `json:"cache_read_input_tokens"`
	Write   int `json:"cache_creation_input_tokens"`
	Details struct {
		Thinking int `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func (s *deepSeekSearch) search(ctx context.Context, query string, limit int) (results []actool.SearchResult, err error) {
	started := time.Now()
	var usage deepSeekUsage
	var meterErr error
	defer func() {
		if s.meter.Store == nil {
			return
		}
		status := "ok"
		if err != nil || meterErr != nil {
			status = "error"
		}
		session := strings.TrimPrefix(transcript.SessionIDFrom(ctx), "exp")
		exp, _, _ := strings.Cut(session, "-")
		if !strings.HasPrefix(transcript.SessionIDFrom(ctx), "exp") {
			exp = ""
		}
		task := llmrec.TaskIDFrom(ctx)
		if task == "" {
			task = exp
		}
		row := &db.LLMUsage{TaskID: task, ExplorationID: db.ParseExpID(exp), Worker: "web_search", Model: strings.TrimSpace(s.cfg.DeepSeekModel), ProfileName: s.meter.ProfileName, LatencyMs: int(time.Since(started).Milliseconds()), InputTokens: usage.Input + usage.Read + usage.Write, OutputTokens: usage.Output, CacheRead: usage.Read, CacheWrite: usage.Write, ReasoningTokens: usage.Details.Thinking, Status: status}
		if e := s.meter.Store.InsertLLMUsage(row); e != nil {
			log.Printf("[llmusage] insert: %v", e)
		}
	}()
	if s.clientErr != nil {
		return nil, s.clientErr
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	maxUses := min(max((limit+9)/10, 1), 3)
	body, _ := json.Marshal(map[string]any{
		"model": strings.TrimSpace(s.cfg.DeepSeekModel), "max_tokens": 1024, "stream": true,
		"messages": []any{map[string]any{"role": "user", "content": "Search the web for: " + query + "\n\nUse the web_search tool. Do not answer from memory; reply with at most one short sentence once the search is done."}},
		"tools":    []any{map[string]any{"type": "web_search_20250305", "name": "web_search", "max_uses": maxUses}},
	})
	endpoint := strings.TrimRight(strings.TrimSpace(s.cfg.DeepSeekBaseURL), "/")
	switch {
	case strings.HasSuffix(endpoint, "/v1/messages"):
	case strings.HasSuffix(endpoint, "/v1"):
		endpoint += "/messages"
	default:
		endpoint += "/v1/messages"
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-api-key", strings.TrimSpace(s.cfg.DeepSeekAPIKey))
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, e := s.client.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("DeepSeek search returned HTTP %d: %s", resp.StatusCode, deepSeekTruncate(strings.TrimSpace(string(raw)), 300))
	}
	// Cancel the actual HTTP read after the hit limit, so even a silent server
	// cannot keep a caller waiting past the bounded usage drain.
	var drain *time.Timer
	defer func() {
		if drain != nil {
			drain.Stop()
		}
	}()
	reachedLimit := false
	fail := func(e error) ([]actool.SearchResult, error) {
		meterErr = e
		if reachedLimit {
			return results, nil
		}
		return nil, e
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 2<<20))
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	seen := map[string]bool{}
	toolErr := ""
	terminalUsage := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var frame struct {
			Type    string         `json:"type"`
			Usage   *deepSeekUsage `json:"usage"`
			Message struct {
				Usage *deepSeekUsage `json:"usage"`
			} `json:"message"`
			ContentBlock struct {
				Type    string `json:"type"`
				Content []struct {
					Title     string `json:"title"`
					URL       string `json:"url"`
					ErrorCode string `json:"error_code"`
				} `json:"content"`
			} `json:"content_block"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &frame) != nil {
			continue
		}
		fold := func(u *deepSeekUsage) {
			if u == nil {
				return
			}
			usage.Input = max(usage.Input, u.Input)
			usage.Output = max(usage.Output, u.Output)
			usage.Read = max(usage.Read, u.Read)
			usage.Write = max(usage.Write, u.Write)
			usage.Details.Thinking = max(usage.Details.Thinking, u.Details.Thinking)
		}
		if frame.Type == "message_start" {
			fold(frame.Message.Usage)
		}
		if frame.Type == "message_delta" {
			terminalUsage = terminalUsage || frame.Usage != nil
			fold(frame.Usage)
		}
		if frame.Type == "message_stop" {
			break
		}
		if frame.Type == "error" && frame.Error.Message != "" {
			return fail(fmt.Errorf("DeepSeek search stream error: %s", frame.Error.Message))
		}
		if frame.Type != "content_block_start" || frame.ContentBlock.Type != "web_search_tool_result" {
			continue
		}
		for _, hit := range frame.ContentBlock.Content {
			if reachedLimit {
				break
			}
			if hit.ErrorCode != "" {
				if toolErr == "" {
					toolErr = hit.ErrorCode
				}
				continue
			}
			if hit.URL == "" || seen[hit.URL] {
				continue
			}
			seen[hit.URL] = true
			title := hit.Title
			if strings.TrimSpace(title) == "" {
				title = hit.URL
				if u, e := url.Parse(hit.URL); e == nil && u.Host != "" {
					title = u.Host
				}
			}
			results = append(results, actool.SearchResult{Title: title, URL: hit.URL, Position: len(results) + 1})
			if len(results) >= limit {
				reachedLimit = true
				drain = time.AfterFunc(s.drainTimeout, cancel)
			}
		}
	}
	if e := sc.Err(); e != nil {
		return fail(fmt.Errorf("could not read DeepSeek search stream: %w", e))
	}
	if e := ctx.Err(); e != nil {
		return fail(fmt.Errorf("could not read DeepSeek search stream: %w", e))
	}
	if len(results) == 0 && toolErr != "" {
		return nil, fmt.Errorf("DeepSeek web search failed: %s", toolErr)
	}
	if !terminalUsage {
		usage = deepSeekUsage{}
	} // Call counted, tokens unknown.
	return results, nil
}

func deepSeekTruncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	half := n / 2
	return s[:half] + fmt.Sprintf("\n\n... [%d characters truncated] ...\n\n", len(s)-n) + s[len(s)-half:]
}
