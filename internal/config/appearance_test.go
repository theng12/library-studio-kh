package config

import "testing"

func TestAppearanceChoices(t *testing.T) {
	if got := Default().Appearance; got != "system" {
		t.Fatalf("default appearance = %q, want system", got)
	}
	for _, choice := range []string{"", "system", "light", "dark", "default"} {
		c := Default()
		c.Appearance = choice
		if err := c.Validate(); err != nil {
			t.Fatalf("appearance %q rejected: %v", choice, err)
		}
	}
	c := Default()
	c.Appearance = "sepia"
	if err := c.Validate(); err == nil {
		t.Fatal("an unknown appearance was accepted")
	}
}
