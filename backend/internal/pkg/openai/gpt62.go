package openai

import (
	"fmt"
	"strings"
)

// GPT62ModelIdentity recognizes the published IDs and local effort/compact
// aliases. Unknown suffixes must not claim another model's capabilities.
func GPT62ModelIdentity(model string) (base, effort string) {
	canonical := CanonicalizeOpenAIModelAliasSpelling(model)
	for _, candidate := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		if canonical == candidate {
			return candidate, ""
		}
		suffix, ok := strings.CutPrefix(canonical, candidate+"-")
		if !ok {
			continue
		}
		switch suffix {
		case "openai-compact":
			return candidate, ""
		case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
			return candidate, suffix
		}
	}
	return "", ""
}

func IsGPT62ModelSpelling(model string) bool {
	base, _ := GPT62ModelIdentity(model)
	return base != ""
}

func ValidateGPT62ReasoningEffort(model, effort string) error {
	base, aliasEffort := GPT62ModelIdentity(model)
	if base == "" {
		return nil
	}
	if aliasEffort == "none" || aliasEffort == "minimal" {
		return fmt.Errorf("%s does not support reasoning effort %q; use low, medium, high, xhigh, max or ultra", base, aliasEffort)
	}
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "", "low", "medium", "high", "xhigh", "max", "ultra":
		return nil
	}
	return fmt.Errorf("%s does not support reasoning effort %q; use low, medium, high, xhigh, max or ultra", base, effort)
}
