package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersionAndUsage(t *testing.T) {
	if code, out, _ := runCLI(t, "", "version"); code != 0 || out != "brandkit 0.1.0\n" {
		t.Fatalf("%d %q", code, out)
	}
	if code, _, _ := runCLI(t, ""); code != 2 {
		t.Fatal("no command should exit 2")
	}
	if code, _, _ := runCLI(t, "", "bogus"); code != 2 {
		t.Fatal("unknown command should exit 2")
	}
}

func TestCheckAndLintExitCodes(t *testing.T) {
	td := filepath.Join("..", "..", "testdata")
	if code, out, errs := runCLI(t, "", "check", filepath.Join(td, "brands", "base")); code != 0 {
		t.Fatalf("check: %d %s %s", code, out, errs)
	}
	cfg := filepath.Join(td, "brands", "base", "export", "lint.json")
	if code, out, _ := runCLI(t, "", "lint", "--config", cfg, filepath.Join(td, "lint", "R2", "pass.html")); code != 0 {
		t.Fatalf("lint pass: %d %s", code, out)
	}
	code, out, _ := runCLI(t, "", "lint", filepath.Join(td, "lint", "R2", "fail.html"), "--config", cfg, "--json")
	if code != 1 {
		t.Fatalf("lint fail: %d %s", code, out)
	}
	var res struct{ Findings []struct{ Rule string } }
	if err := json.Unmarshal([]byte(out), &res); err != nil || len(res.Findings) != 4 {
		t.Fatalf("json: %v %s", err, out)
	}
}

func hookInput(t *testing.T, file string) string {
	abs, _ := filepath.Abs(file)
	b, _ := json.Marshal(map[string]any{"cwd": filepath.Dir(abs), "tool_name": "Edit", "tool_input": map[string]any{"file_path": abs}})
	return string(b)
}

// TestHookWarns: the edit hook reports a fixture's findings to the agent
// and exits 0 (story 5).
func TestHookWarns(t *testing.T) {
	cfg, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "brands", "base", "export", "lint.json"))
	code, out, _ := runCLI(t, hookInput(t, filepath.Join("..", "..", "testdata", "lint", "R7", "fail.html")), "lint", "--hook", "--config", cfg)
	if code != 0 {
		t.Fatalf("hook exit %d", code)
	}
	var msg struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &msg); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if msg.HookSpecificOutput.HookEventName != "PostToolUse" || !strings.Contains(msg.HookSpecificOutput.AdditionalContext, "R7") {
		t.Fatalf("%+v", msg)
	}
	// A clean file, a file lint doesn't read, and junk input: silent, exit 0.
	for _, in := range []string{hookInput(t, filepath.Join("..", "..", "testdata", "lint", "R7", "pass.html")), hookInput(t, "main.go"), "not json"} {
		if code, out, _ := runCLI(t, in, "lint", "--hook", "--config", cfg); code != 0 || out != "" {
			t.Errorf("input %q: %d %q", in, code, out)
		}
	}
}

// TestPluginHook runs the command in agent-plugin/hooks/hooks.json: with
// brandkit on PATH it warns; without brandkit or mise it fails open.
func TestPluginHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh hook")
	}
	var hooks struct {
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct {
				Command string
				Args    []string
			}
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "agent-plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &hooks); err != nil {
		t.Fatal(err)
	}
	h := hooks.Hooks["PostToolUse"][0]
	if !strings.Contains(h.Matcher, "Edit") || !strings.Contains(h.Matcher, "Write") {
		t.Fatalf("matcher %q", h.Matcher)
	}
	cmdSpec := h.Hooks[0]

	bin := t.TempDir()
	build := exec.Command("go", "build", "-o", filepath.Join(bin, "brandkit"), ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	// A project with the brand installed (a lock pointing at lint.json).
	project := t.TempDir()
	lj, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "brands", "base", "export", "lint.json"))
	os.MkdirAll(filepath.Join(project, "vendor", "brand", "b", "export"), 0o755)
	os.WriteFile(filepath.Join(project, "vendor", "brand", "b", "export", "lint.json"), lj, 0o644)
	os.WriteFile(filepath.Join(project, "brandkit.lock"), []byte(`{"version":1,"brands":[{"repo":"example/b","tag":"v1","commit":"x","dir":"vendor/brand/b","lint":"vendor/brand/b/export/lint.json","files":{}}]}`), 0o644)
	src, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "lint", "R3", "fail.gohtml"))
	page := filepath.Join(project, "templates", "page.gohtml")
	os.MkdirAll(filepath.Dir(page), 0o755)
	os.WriteFile(page, src, 0o644)
	in, _ := json.Marshal(map[string]any{"cwd": project, "tool_input": map[string]any{"file_path": page}})

	runHook := func(path string) (string, int) {
		c := exec.Command(sh, cmdSpec.Args...)
		c.Env = []string{"PATH=" + path, "HOME=" + t.TempDir()}
		c.Stdin = bytes.NewReader(in)
		out, _ := c.Output()
		return string(out), c.ProcessState.ExitCode()
	}
	shDir := filepath.Dir(sh)
	out, code := runHook(bin + ":" + shDir + ":/usr/bin:/bin")
	if code != 0 || !strings.Contains(out, "R3") || !strings.Contains(out, "bg-blue-500") {
		t.Fatalf("with brandkit: exit %d, %q", code, out)
	}
	out, code = runHook(shDir)
	if code != 0 || out != "" {
		t.Fatalf("without brandkit: exit %d, %q", code, out)
	}
}
