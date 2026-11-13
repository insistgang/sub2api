package antigravity

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDefaultModels_ContainsNewAndLegacyImageModels(t *testing.T) {
	t.Parallel()

	models := DefaultModels()
	byID := make(map[string]ClaudeModel, len(models))
	for _, m := range models {
		byID[m.ID] = m
	}

	requiredIDs := []string{
		"claude-fable-5-1",
		"claude-fable-5",
		"claude-opus-4-8",
		"claude-opus-4-6-thinking",
		"gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview",
		"gemini-3-pro-image", // legacy compatibility
		"gemini-3.6-flash",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-low",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-tiered",
		"gemini-3.7-flash",
		"gemini-3.7-flash-high",
		"gemini-3.7-flash-low",
		"gemini-3.7-flash-medium",
		"gemini-3.7-flash-tiered",
		"gemini-3.8-flash",
		"gemini-3.8-flash-high",
		"gemini-3.8-flash-low",
		"gemini-3.8-flash-medium",
		"gemini-3.8-flash-tiered",
		"gemini-4-argon-high",
		"gemini-4-argon-low",
		"gemini-4-argon-medium",
		"claude-opus-5-5-high",
		"claude-opus-5-5-low",
		"claude-opus-5-5-medium",
		"claude-sonnet-5-5-high",
		"claude-sonnet-5-5-low",
		"claude-sonnet-5-5-medium",
	}

	for _, id := range requiredIDs {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected model %q to be exposed in DefaultModels", id)
		}
	}
}

func TestDefaultModels_ExposesHydrogenReasoningVariants(t *testing.T) {
	claudeIDs := make(map[string]bool)
	createdAt := make(map[string]string)
	for _, model := range DefaultModels() {
		claudeIDs[model.ID] = true
		createdAt[model.ID] = model.CreatedAt
	}
	geminiIDs := make(map[string]bool)
	for _, model := range DefaultGeminiModels() {
		geminiIDs[model.Name] = true
	}
	for _, id := range []string{
		"gemini-4-hydrogen-low", "gemini-4-hydrogen-medium",
		"gemini-4-hydrogen-high", "gemini-4-hydrogen-xhigh",
	} {
		if !claudeIDs[id] || !geminiIDs["models/"+id] {
			t.Errorf("Hydrogen variant %q missing from a public catalog", id)
		}
		if !IsGeminiReasoningModel(id) {
			t.Errorf("Hydrogen variant %q must omit unsupported sampling/stop parameters", id)
		}
		if _, err := time.Parse(time.RFC3339, createdAt[id]); err != nil {
			t.Errorf("Hydrogen variant %q must provide an SDK-compatible created_at timestamp", id)
		}
	}
	if !IsGeminiReasoningModel("gemini-4-hydrogen") {
		t.Error("bare Hydrogen alias must be recognized as reasoning")
	}
	if claudeIDs["gemini-4-hydrogen"] || geminiIDs["models/gemini-4-hydrogen"] {
		t.Error("public Antigravity catalogs must expose actual Hydrogen variants, not the bare alias")
	}
}

func TestClaudeContentItem_ThinkingAlwaysHasSignature(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(ClaudeContentItem{Type: "thinking", Thinking: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"thinking":"plan","signature":"","type":"thinking"}`; got != want {
		t.Fatalf("thinking block = %s, want %s", got, want)
	}

	data, err = json.Marshal(ClaudeContentItem{Type: "text", Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"type":"text","text":"hi"}`; got != want {
		t.Fatalf("text block = %s, want %s", got, want)
	}
}

func TestIsGeminiReasoningModel_IncludesProAgent(t *testing.T) {
	t.Parallel()

	if !IsGeminiReasoningModel("gemini-pro-agent") {
		t.Fatal("expected gemini-pro-agent to be recognized as a Gemini reasoning model")
	}
	if !IsGeminiReasoningModel("gemini-3.8-flash-high") {
		t.Fatal("expected gemini-3.8-flash-high to be recognized as a Gemini reasoning model")
	}
	if !IsGeminiReasoningModel("gemini-4-argon") {
		t.Fatal("expected gemini-4-argon to be recognized as a Gemini reasoning model")
	}
	if !IsGeminiReasoningModel("gemini-4-argon-high") {
		t.Fatal("expected gemini-4-argon-high to be recognized as a Gemini reasoning model")
	}
	if IsGeminiReasoningModel("gemini-2.5-flash") {
		t.Fatal("did not expect gemini-2.5-flash to be recognized as a Gemini reasoning model")
	}
}
