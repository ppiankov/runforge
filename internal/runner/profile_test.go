package runner

import (
	"sort"
	"testing"
)

func TestResolveEnv_Nil(t *testing.T) {
	got, err := ResolveEnv(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestResolveEnv_LiteralValues(t *testing.T) {
	input := map[string]string{
		"API_URL": "https://example.com",
		"TIMEOUT": "3000",
	}
	got, err := ResolveEnv(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["API_URL"] != "https://example.com" || got["TIMEOUT"] != "3000" {
		t.Fatalf("unexpected result: %v", got)
	}
}

func TestResolveEnv_EnvReference(t *testing.T) {
	t.Setenv("TEST_SECRET_KEY", "my-secret")

	input := map[string]string{
		"API_KEY": "env:TEST_SECRET_KEY",
	}
	got, err := ResolveEnv(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["API_KEY"] != "my-secret" {
		t.Fatalf("expected 'my-secret', got %q", got["API_KEY"])
	}
}

func TestResolveEnv_MissingEnvVar(t *testing.T) {
	t.Setenv("NONEXISTENT_VAR_FOR_TEST", "")

	input := map[string]string{
		"API_KEY": "env:NONEXISTENT_VAR_FOR_TEST",
	}
	_, err := ResolveEnv(input)
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}

func TestMapToEnvSlice_Nil(t *testing.T) {
	got := MapToEnvSlice(nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestMapToEnvSlice_Values(t *testing.T) {
	input := map[string]string{
		"A": "1",
		"B": "2",
	}
	got := MapToEnvSlice(input)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Fatalf("unexpected result: %v", got)
	}
}
