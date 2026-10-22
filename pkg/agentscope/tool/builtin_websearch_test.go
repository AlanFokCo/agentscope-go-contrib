package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
)

func TestWebSearchTool_Results(t *testing.T) {
	var gotQuery string
	var gotLimit int
	toolUnderTest := NewWebSearchTool(func(_ context.Context, query string, limit int) ([]SearchResult, error) {
		gotQuery, gotLimit = query, limit
		return []SearchResult{{Title: "Go", URL: "https://go.dev", Snippet: "The Go language"}}, nil
	})

	resp, err := toolUnderTest.Execute(context.Background(), map[string]any{"query": "golang", "limit": float64(99)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.State != message.ToolResultSuccess {
		t.Fatalf("unexpected tool error state: %v", resp.State)
	}
	if gotQuery != "golang" {
		t.Errorf("query not passed through: %q", gotQuery)
	}
	if gotLimit != maxSearchLimit {
		t.Errorf("limit not clamped: %d", gotLimit)
	}
	var payload struct {
		Results []SearchResult `json:"results"`
	}
	tb, ok := resp.Content[0].(message.TextBlock)
	if !ok {
		t.Fatalf("unexpected content block: %T", resp.Content[0])
	}
	text := tb.Text
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if len(payload.Results) != 1 || payload.Results[0].URL != "https://go.dev" {
		t.Errorf("results lost: %s", text)
	}
}

func TestWebSearchTool_ValidationAndError(t *testing.T) {
	searchErr := errors.New("provider down")
	toolUnderTest := NewWebSearchTool(func(context.Context, string, int) ([]SearchResult, error) {
		return nil, searchErr
	})

	resp, _ := toolUnderTest.Execute(context.Background(), map[string]any{"query": "  "})
	if resp == nil || resp.State != message.ToolResultError {
		t.Error("empty query must be a tool error")
	}
	resp, _ = toolUnderTest.Execute(context.Background(), map[string]any{"query": "x"})
	if resp == nil || resp.State != message.ToolResultError ||
		!strings.Contains(resp.Content[0].(message.TextBlock).Text, "provider down") {
		t.Errorf("searcher error must surface as a tool error: %+v", resp)
	}
}
