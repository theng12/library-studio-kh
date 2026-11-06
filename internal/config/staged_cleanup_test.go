package config

import "testing"

func TestStagedCleanupDefaultsOffAndSane(t *testing.T) {
	c := Default()
	if c.StagedCleanupOn || c.CleanupOn {
		t.Fatal("staged cleanup and legacy cleanup must start off")
	}
	if c.StagedDays != 3 || c.ManagedTrashDays != 4 || c.NativeTrashDays != 30 {
		t.Fatalf("unexpected retention defaults: %+v", c)
	}
	if c.PressureFreeGB != 50 || c.SeedRatio != 2 || c.SeedMinutes != 2880 {
		t.Fatalf("unexpected pressure/seeding defaults: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

func TestStagedCleanupValidation(t *testing.T) {
	valid := Default()
	valid.StagedCleanupOn = true
	valid.LibraryPath = "/library"
	if err := valid.Validate(); err != nil {
		t.Fatalf("enabled policy with a library folder must validate: %v", err)
	}
	cases := []struct {
		name string
		edit func(*Config)
	}{
		{"enabled without library", func(c *Config) { c.LibraryPath = "" }},
		{"staged days", func(c *Config) { c.StagedDays = 0 }},
		{"managed days", func(c *Config) { c.ManagedTrashDays = -1 }},
		{"native days", func(c *Config) { c.NativeTrashDays = 0 }},
		{"pressure", func(c *Config) { c.PressureFreeGB = -1 }},
		{"seed ratio", func(c *Config) { c.SeedRatio = -1 }},
		{"seed minutes", func(c *Config) { c.SeedMinutes = -1 }},
	}
	for _, tc := range cases {
		c := valid
		tc.edit(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: invalid policy accepted", tc.name)
		}
	}
}

func TestStagedCleanupSurvivesOpenWithDefaults(t *testing.T) {
	home := t.TempDir()
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	c := store.Get()
	c.StagedCleanupOn = true
	c.LibraryPath = home
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	again, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Get().StagedCleanupOn || again.Get().StagedDays != 3 {
		t.Fatalf("staged settings not kept: %+v", again.Get())
	}
}
