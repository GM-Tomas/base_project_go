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
