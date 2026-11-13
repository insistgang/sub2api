package openai

import (
	"fmt"
	"strings"
)

// IsGPT61TerraModelSpelling includes invalid local effort aliases so they can be
// rejected before compatibility conversion silently changes their intent.
func IsGPT61TerraModelSpelling(model string) bool {
	canonical := CanonicalizeOpenAIModelAliasSpelling(model)
	if canonical == "gpt-6.1-terra" {
		return true
	}
	suffix, ok := strings.CutPrefix(canonical, "gpt-6.1-terra-")
	if !ok {
		return false
	}
	switch suffix {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "openai-compact":
		return true
	}
	return false
}
func ValidateGPT61TerraReasoningEffort(model, effort string) error {
	if !IsGPT61TerraModelSpelling(model) {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "", "none", "low", "medium", "high", "xhigh", "max":
		return nil
	}
	return fmt.Errorf("gpt-6.1-terra does not support reasoning effort %q; use none, low, medium, high, xhigh or max", effort)
}

// ValidateModelReasoningEffort retains existing Sol validation and adds Terra's
// distinct API reasoning scale. Codex ultra remains a client orchestration mode.
func ValidateModelReasoningEffort(model, effort string) error {
	if err := ValidateGPT61TerraReasoningEffort(model, effort); err != nil {
		return err
	}
	return ValidateGPT61SolReasoningEffort(model, effort)
}

func RequiresResponsesForToolCalls(model string) bool {
	return IsGPT61SolModelSpelling(model) || IsGPT61TerraModelSpelling(model)
}
