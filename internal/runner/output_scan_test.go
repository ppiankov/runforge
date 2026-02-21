package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeKey builds a fake credential string at runtime to avoid
// triggering pre-commit secret detection hooks on the test file itself.
func fakeKey(prefix string, length int) string {
	body := strings.Repeat("ab12cd34ef", length/10+1)
	return prefix + body[:length]
}

func TestScanOutputGroqKey(t *testing.T) {
	key := fakeKey("gsk_", 24)
	input := "config loaded: " + key
	result, count := ScanOutput(input)
	if count != 1 {
		t.Errorf("expected 1 secret, got %d", count)
	}
	if strings.Contains(result, "gsk_") {
		t.Error("groq key not redacted")
	}
}

func TestScanOutputOpenAIKey(t *testing.T) {
	key := fakeKey("sk-test-", 24)
	input := "key: " + key
	result, count := ScanOutput(input)
	if count != 1 {
		t.Errorf("expected 1 secret, got %d", count)
	}
	if strings.Contains(result, key) {
		t.Error("openai key not redacted")
	}
}

func TestScanOutputBearerToken(t *testing.T) {
	token := fakeKey("", 30)
	input := "Authorization: Bearer " + token
	result, count := ScanOutput(input)
	if count != 1 {
		t.Errorf("expected 1 secret, got %d", count)
	}
	if strings.Contains(result, token) {
		t.Error("bearer token not redacted")
	}
}

func TestScanOutputGitHubToken(t *testing.T) {
	// Our scanner pattern requires 36+ chars after ghp_ prefix.
	// Build a fake one at runtime.
	key := fakeKey("ghp_", 40)
	input := "token: " + key
	result, count := ScanOutput(input)
	if count != 1 {
		t.Errorf("expected 1 secret, got %d", count)
	}
	if strings.Contains(result, "ghp_") {
		t.Error("github token not redacted")
	}
}

func TestScanOutputAWSKey(t *testing.T) {
	// AWS pattern: AKIA + 16 uppercase alphanumeric
	key := "AKIA" + strings.Repeat("ABCD", 4)
	input := "aws_access_key_id = " + key
	result, count := ScanOutput(input)
	if count != 1 {
		t.Errorf("expected 1 secret, got %d", count)
	}
	if strings.Contains(result, key) {
		t.Error("AWS key not redacted")
	}
}

func TestScanOutputNoSecrets(t *testing.T) {
	input := "uname -a\nLinux host 5.15.0\n"
	result, count := ScanOutput(input)
	if count != 0 {
		t.Errorf("expected 0 secrets, got %d", count)
	}
	if result != input {
		t.Error("clean output was modified")
	}
}

func TestScanOutputMultiple(t *testing.T) {
	key1 := fakeKey("gsk_", 24)
	key2 := fakeKey("sk-test-", 24)
	input := fmt.Sprintf("key1: %s\nkey2: %s", key1, key2)
	_, count := ScanOutput(input)
	if count != 2 {
		t.Errorf("expected 2 secrets, got %d", count)
	}
}

func TestScanOutputFullEnvLine(t *testing.T) {
	input := "declare -x GROQ_API_KEY=fakeval123\nexport NULLBOT_PROFILE=vm-cloud\n"
	result, count := ScanOutputFull(input)
	if count < 2 {
		t.Errorf("expected at least 2 matches, got %d", count)
	}
	if strings.Contains(result, "fakeval123") {
		t.Error("groq key value not redacted")
	}
}

func TestScanOutputDirRedactsFiles(t *testing.T) {
	dir := t.TempDir()

	key := fakeKey("gsk_", 24)
	content := "output: " + key
	if err := os.WriteFile(filepath.Join(dir, "output.log"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	clean := "uname -a\nLinux host 5.15.0\n"
	if err := os.WriteFile(filepath.Join(dir, "stderr.log"), []byte(clean), 0o644); err != nil {
		t.Fatal(err)
	}

	leaks := ScanOutputDir(dir)
	if leaks != 1 {
		t.Errorf("expected 1 leak, got %d", leaks)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "output.log"))
	if strings.Contains(string(data), "gsk_") {
		t.Error("output.log not redacted in place")
	}

	data, _ = os.ReadFile(filepath.Join(dir, "stderr.log"))
	if string(data) != clean {
		t.Error("clean file was modified")
	}
}

func TestScanOutputDirSkipsNonTextFiles(t *testing.T) {
	dir := t.TempDir()

	key := fakeKey("gsk_", 24)
	if err := os.WriteFile(filepath.Join(dir, "data.bin"), []byte(key), 0o644); err != nil {
		t.Fatal(err)
	}

	leaks := ScanOutputDir(dir)
	if leaks != 0 {
		t.Errorf("expected 0 leaks (should skip .bin), got %d", leaks)
	}
}
