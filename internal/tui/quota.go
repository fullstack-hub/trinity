package tui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/fullstack-hub/trinity/internal/client"
)

// QuotaDisplay holds displayable quota info for the sidebar.
type QuotaDisplay struct {
	Label string // main value (bright), e.g. "23%"
	Sub   string // supplementary info (dim), e.g. "(2h31m)"
	Pct   int    // primary percentage for progress bar (0–100), -1 = no bar
}

// QuotaTracker tracks usage across all services.
type QuotaTracker struct {
	mu sync.Mutex

	// Claude: from Anthropic OAuth /usage API
	claude5hPct    int
	claude5hResets string // ISO 8601
	claudeWkPct    int
	claudeWkResets string // ISO 8601

	// Copilot: from account.getQuota RPC via /quota endpoint
	copilotUsed      int
	copilotLimit     int
	copilotRemainPct float64
	copilotResetDate string
	copilotUnlimited bool

	// Gemini: from /stats endpoint (refreshUserQuota API)
	geminiBuckets []client.GeminiStatsBucket

	claudeClient  *client.Client
	copilotClient *client.Client
	geminiClient  *client.Client
}

func NewQuotaTracker(claudeClient, copilotClient, geminiClient *client.Client) *QuotaTracker {
	return &QuotaTracker{
		copilotLimit:  1000,
		claudeClient:  claudeClient,
		copilotClient: copilotClient,
		geminiClient:  geminiClient,
	}
}

// ── Claude (Anthropic OAuth API) ──

// FetchClaudeUsage calls the Claude server's /usage endpoint.
func (q *QuotaTracker) FetchClaudeUsage(ctx context.Context) {
	if q.claudeClient == nil {
		return
	}
	usage, err := q.claudeClient.Usage(ctx)
	if err != nil || usage == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claude5hPct = int(math.Round(usage.FiveHourPct))
	q.claude5hResets = usage.FiveHourResets
	q.claudeWkPct = int(math.Round(usage.WeeklyPct))
	q.claudeWkResets = usage.WeeklyResets
}

// Claude5h returns 5-hour rolling window quota.
func (q *QuotaTracker) Claude5h() QuotaDisplay {
	q.mu.Lock()
	defer q.mu.Unlock()
	sub := ""
	if r := formatResetTime(q.claude5hResets); r != "" {
		sub = fmt.Sprintf("(%s)", r)
	}
	return QuotaDisplay{Label: fmt.Sprintf("%d%%", q.claude5hPct), Sub: sub, Pct: q.claude5hPct}
}

// ClaudeWeekly returns weekly rolling window quota.
func (q *QuotaTracker) ClaudeWeekly() QuotaDisplay {
	q.mu.Lock()
	defer q.mu.Unlock()
	sub := ""
	if r := formatResetTime(q.claudeWkResets); r != "" {
		sub = fmt.Sprintf("(%s)", r)
	}
	return QuotaDisplay{Label: fmt.Sprintf("%d%%", q.claudeWkPct), Sub: sub, Pct: q.claudeWkPct}
}

// ── Copilot (account.getQuota RPC via /quota endpoint) ──

// IncrCopilot adds premium request cost locally.
func (q *QuotaTracker) IncrCopilot(cost int) {
	if cost <= 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.copilotUsed += cost
}

func (q *QuotaTracker) Copilot() QuotaDisplay {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.copilotUnlimited {
		return QuotaDisplay{Label: "Unlimited", Pct: -1}
	}

	// Use remaining percentage from server API
	usedPct := int(math.Round(100 - q.copilotRemainPct))
	if usedPct < 0 {
		usedPct = 0
	}
	if usedPct > 100 {
		usedPct = 100
	}

	main := fmt.Sprintf("%.1f%%", q.copilotRemainPct)
	var subs []string
	if q.copilotLimit > 0 {
		subs = append(subs, fmt.Sprintf("(%d/%d)", q.copilotUsed, q.copilotLimit))
	}
	if r := formatResetTime(nextMonthFirst()); r != "" {
		subs = append(subs, fmt.Sprintf("(%s)", r))
	}

	return QuotaDisplay{Label: main, Sub: strings.Join(subs, " "), Pct: usedPct}
}

// FetchCopilotQuota calls the Copilot server's /quota endpoint (account.getQuota RPC).
func (q *QuotaTracker) FetchCopilotQuota(ctx context.Context) {
	if q.copilotClient == nil {
		return
	}
	quota, err := q.copilotClient.Quota(ctx)
	if err != nil || quota == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.copilotRemainPct = quota.RemainingPct
	q.copilotUsed = quota.Used
	q.copilotLimit = quota.Limit
	q.copilotResetDate = quota.ResetDate
	q.copilotUnlimited = quota.Unlimited
}

// ── Gemini (from /stats endpoint — retrieveUserQuota API) ──

// FetchGeminiStats calls the Gemini server's /stats endpoint.
func (q *QuotaTracker) FetchGeminiStats(ctx context.Context) {
	if q.geminiClient == nil {
		return
	}
	stats, err := q.geminiClient.Stats(ctx)
	if err != nil || stats == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.geminiBuckets = stats.Buckets
}

// Gemini returns the primary Gemini model quota for sidebar display.
func (q *QuotaTracker) Gemini() QuotaDisplay {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.geminiBuckets) == 0 {
		return QuotaDisplay{Label: "no data", Pct: -1}
	}

	// Find the primary model (prefer pro, fallback to first)
	var best *client.GeminiStatsBucket
	for i := range q.geminiBuckets {
		b := &q.geminiBuckets[i]
		if best == nil {
			best = b
		}
		// Prefer pro model for display
		if len(b.ModelID) > 0 && (contains(b.ModelID, "pro") || contains(b.ModelID, "3.1")) {
			best = b
		}
	}
	if best == nil {
		return QuotaDisplay{Label: "no data", Pct: -1}
	}

	usedPct := int(math.Round(100 - best.RemainingPct))
	if usedPct < 0 {
		usedPct = 0
	}
	if usedPct > 100 {
		usedPct = 100
	}

	label := fmt.Sprintf("%.1f%% remaining", best.RemainingPct)
	if r := formatResetTime(best.ResetTime); r != "" {
		label += fmt.Sprintf(" (%s)", r)
	}

	return QuotaDisplay{Label: label, Pct: usedPct}
}

// GeminiBuckets returns all per-model Gemini quota buckets for detailed display.
func (q *QuotaTracker) GeminiBuckets() []client.GeminiStatsBucket {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]client.GeminiStatsBucket, len(q.geminiBuckets))
	copy(out, q.geminiBuckets)
	return out
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ── Helpers ──

// formatResetTime converts ISO 8601 to "3h44m" or "2d5h" style.
func formatResetTime(isoStr string) string {
	if isoStr == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, isoStr)
	if err != nil {
		return ""
	}
	diff := time.Until(t)
	if diff <= 0 {
		return ""
	}
	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	mins := int(diff.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	return fmt.Sprintf("%dh%dm", hours, mins)
}

// nextMonthFirst returns the ISO 8601 string for the 1st of next month (UTC).
func nextMonthFirst() string {
	now := time.Now().UTC()
	y, m, _ := now.Date()
	next := time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
	return next.Format(time.RFC3339)
}
