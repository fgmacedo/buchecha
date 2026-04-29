package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyEnv_SetsFromVarsWhenShellMissing(t *testing.T) {
	key := "BUCHECHA_TEST_FROMVAR"
	os.Unsetenv(key)
	t.Cleanup(func() { os.Unsetenv(key) })

	c := &Config{Env: Env{Vars: map[string]string{key: "hello"}}}
	if err := c.ApplyEnv(nil); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv(key); got != "hello" {
		t.Errorf("env = %q, want hello", got)
	}
}

func TestApplyEnv_ShellWinsOverConfigVars(t *testing.T) {
	key := "BUCHECHA_TEST_SHELLWINS"
	t.Setenv(key, "from-shell")

	c := &Config{Env: Env{Vars: map[string]string{key: "from-config"}}}
	if err := c.ApplyEnv(nil); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv(key); got != "from-shell" {
		t.Errorf("env = %q, want from-shell", got)
	}
}

func TestApplyEnv_ExtraFlagsOverrideEverything(t *testing.T) {
	key := "BUCHECHA_TEST_FLAG"
	t.Setenv(key, "from-shell")

	c := &Config{Env: Env{Vars: map[string]string{key: "from-config"}}}
	if err := c.ApplyEnv([]string{key + "=from-flag"}); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv(key); got != "from-flag" {
		t.Errorf("env = %q, want from-flag", got)
	}
}

func TestApplyEnv_LoadsDotEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	contents := "BUCHECHA_TEST_FILE=fromfile\n# a comment\nBUCHECHA_TEST_QUOTED=\"quoted value\"\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Unsetenv("BUCHECHA_TEST_FILE")
		os.Unsetenv("BUCHECHA_TEST_QUOTED")
	})
	os.Unsetenv("BUCHECHA_TEST_FILE")
	os.Unsetenv("BUCHECHA_TEST_QUOTED")

	c := &Config{Env: Env{Files: []string{path}}}
	if err := c.ApplyEnv(nil); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv("BUCHECHA_TEST_FILE"); got != "fromfile" {
		t.Errorf("env = %q, want fromfile", got)
	}
	if got := os.Getenv("BUCHECHA_TEST_QUOTED"); got != "quoted value" {
		t.Errorf("quoted env = %q, want %q", got, "quoted value")
	}
}

func TestApplyEnv_FileLaterFileWinsAmongFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.env")
	b := filepath.Join(dir, "b.env")
	if err := os.WriteFile(a, []byte("BUCHECHA_TEST_ORDER=fromA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("BUCHECHA_TEST_ORDER=fromB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := "BUCHECHA_TEST_ORDER"
	os.Unsetenv(key)
	t.Cleanup(func() { os.Unsetenv(key) })

	c := &Config{Env: Env{Files: []string{a, b}}}
	if err := c.ApplyEnv(nil); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv(key); got != "fromB" {
		t.Errorf("env = %q, want fromB (later file wins)", got)
	}
}

func TestApplyEnv_VarsOverrideFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("BUCHECHA_TEST_VAROVER=fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := "BUCHECHA_TEST_VAROVER"
	os.Unsetenv(key)
	t.Cleanup(func() { os.Unsetenv(key) })

	c := &Config{Env: Env{
		Files: []string{path},
		Vars:  map[string]string{key: "fromvar"},
	}}
	if err := c.ApplyEnv(nil); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv(key); got != "fromvar" {
		t.Errorf("env = %q, want fromvar (Vars override Files)", got)
	}
}

func TestApplyEnv_MissingFileSkipsSilently(t *testing.T) {
	c := &Config{Env: Env{Files: []string{"/nonexistent/path/.env"}}}
	if err := c.ApplyEnv(nil); err != nil {
		t.Fatalf("ApplyEnv should ignore missing files, got: %v", err)
	}
}

func TestApplyEnv_TildeExpansion(t *testing.T) {
	key := "BUCHECHA_TEST_TILDE"
	os.Unsetenv(key)
	t.Cleanup(func() { os.Unsetenv(key) })

	c := &Config{Env: Env{Vars: map[string]string{key: "~/foo"}}}
	if err := c.ApplyEnv(nil); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	want := filepath.Join(home, "foo")
	if got := os.Getenv(key); got != want {
		t.Errorf("env = %q, want %q", got, want)
	}
}

func TestApplyEnv_VarReferenceExpansion(t *testing.T) {
	upstream := "BUCHECHA_TEST_UPSTREAM"
	t.Setenv(upstream, "VALUE")

	key := "BUCHECHA_TEST_REF"
	os.Unsetenv(key)
	t.Cleanup(func() { os.Unsetenv(key) })

	c := &Config{Env: Env{Vars: map[string]string{key: "${" + upstream + "}/x"}}}
	if err := c.ApplyEnv(nil); err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if got := os.Getenv(key); got != "VALUE/x" {
		t.Errorf("env = %q, want VALUE/x", got)
	}
}

func TestApplyEnv_InvalidFlagFormat(t *testing.T) {
	c := &Config{}
	err := c.ApplyEnv([]string{"NOEQUALS"})
	if err == nil {
		t.Errorf("expected error on invalid flag")
	}
}
