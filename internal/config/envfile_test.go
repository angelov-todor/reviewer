package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude.env")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLoadEnvFileMakesAManualRunMatchTheService is the footgun this closes.
//
// `claude setup-token` prints a long-lived token rather than storing one, so
// on a service box it lives in a file systemd hands to the daemon. Without
// loading it here too, the same machine works as a service and looks broken
// by hand: doctor run by an operator sees no token, reports that claude has
// no credentials, and sends them hunting a fault that is not there. That
// happened once already.
func TestLoadEnvFileMakesAManualRunMatchTheService(t *testing.T) {
	c := Default()
	c.EnvFile = writeEnv(t, "# a comment\n\nCLAUDE_CODE_OAUTH_TOKEN=sk-test-value\nexport OTHER=two\n")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("OTHER", "")

	n, err := c.LoadEnvFile()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("set %d variables, want 2 (comments and blank lines ignored)", n)
	}
	if got := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); got != "sk-test-value" {
		t.Errorf("token = %q", got)
	}
	// `export KEY=value` is what people write in a file they also source by
	// hand, so it is accepted rather than silently skipped.
	if got := os.Getenv("OTHER"); got != "two" {
		t.Errorf("export form not handled: %q", got)
	}
}

// An existing value wins. Under systemd the same values arrive through
// EnvironmentFile first, and an operator who exported one for a single
// command meant it.
func TestLoadEnvFileNeverOverwrites(t *testing.T) {
	c := Default()
	c.EnvFile = writeEnv(t, "CLAUDE_CODE_OAUTH_TOKEN=from-file\n")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "already-set")

	if _, err := c.LoadEnvFile(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); got != "already-set" {
		t.Errorf("the environment must win, got %q", got)
	}
}

// A token with = in it survives, and surrounding quotes come off. Both are
// real shapes for a credential, and a parser that mangles one is worse than
// no parser.
func TestLoadEnvFileKeepsTheValueIntact(t *testing.T) {
	c := Default()
	c.EnvFile = writeEnv(t, "A=\"quoted=value==\"\nB=bare=value\n")
	t.Setenv("A", "")
	t.Setenv("B", "")

	if _, err := c.LoadEnvFile(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("A"); got != "quoted=value==" {
		t.Errorf("A = %q", got)
	}
	if got := os.Getenv("B"); got != "bare=value" {
		t.Errorf("B = %q", got)
	}
}

// A configured file that is not there is an error, not a shrug: the operator
// said the credentials live there.
func TestLoadEnvFileReportsAMissingFile(t *testing.T) {
	c := Default()
	c.EnvFile = filepath.Join(t.TempDir(), "absent.env")
	if _, err := c.LoadEnvFile(); err == nil {
		t.Fatal("a configured env_file that does not exist must be an error")
	}
}

func TestEnvFileMustBeAbsolute(t *testing.T) {
	c := usable(Default())
	c.Space = "spaces/x"
	c.EnvFile = "claude.env"
	if err := c.Validate(); err == nil {
		t.Fatal("a relative env_file must be refused")
	}
}
