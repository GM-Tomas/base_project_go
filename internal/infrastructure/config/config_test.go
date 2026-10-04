package config

import "testing"

func TestDevUserIDIgnoredOnVercel(t *testing.T) {
	t.Setenv("AUTH_DEV_USER_ID", "00000000-0000-0000-0000-000000000001")

	t.Setenv("VERCEL", "")
	if LoadConfig().DevUserID == "" {
		t.Fatal("expected dev user locally")
	}

	t.Setenv("VERCEL", "1")
	if got := LoadConfig().DevUserID; got != "" {
		t.Fatalf("dev auth must be off on Vercel, got %q", got)
	}
}

func TestPreviewOnlyOnVercelPreviews(t *testing.T) {
	for _, c := range []struct {
		vercel, env, target string
		preview             bool
	}{
		{"1", "preview", "preview", true},
		{"1", "preview", "", true},         // older deployments without VERCEL_TARGET_ENV
		{"1", "preview", "staging", false}, // a custom environment has its own database
		{"1", "production", "production", false},
		{"1", "development", "development", false}, // vercel dev
		{"1", "", "", false},
		{"", "preview", "preview", false}, // not a Vercel deployment (a pulled .env sets VERCEL too)
	} {
		t.Setenv("VERCEL", c.vercel)
		t.Setenv("VERCEL_ENV", c.env)
		t.Setenv("VERCEL_TARGET_ENV", c.target)
		if got := LoadConfig().Preview; got != c.preview {
			t.Errorf("VERCEL=%q VERCEL_ENV=%q VERCEL_TARGET_ENV=%q: Preview = %v, want %v", c.vercel, c.env, c.target, got, c.preview)
		}
	}
}
