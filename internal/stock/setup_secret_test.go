package stock

import "testing"

func TestSetupSavedSecretComparisonPreservesWhitespace(t *testing.T) {
	secret := " intentional surrounding spaces "
	raw := map[string]any{"fields": []any{map[string]any{"name": "password", "value": secret}}}
	if got := setupSecretStatus(raw, "password", secret); got != CheckPassed {
		t.Fatal("saved secret was changed before comparison")
	}
	if got := setupSecretStatus(raw, "password", "intentional surrounding spaces"); got != CheckFailed {
		t.Fatal("a different secret was accepted")
	}
}
