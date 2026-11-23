package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// testdataDir is absolute, for tests that change folder.
var testdataDir, _ = filepath.Abs(filepath.Join("..", "..", "testdata"))

// brandProject makes a project with the base brand's lint.json installed
// (a lock pointing at it) and returns its folder.
func brandProject(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	lj, err := os.ReadFile(filepath.Join(testdataDir, "brands", "base", "export", "lint.json"))
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(project, "vendor", "brand", "b", "export"), 0o755)
	os.WriteFile(filepath.Join(project, "vendor", "brand", "b", "export", "lint.json"), lj, 0o644)
	sum := sha256.Sum256(lj)
	lock := fmt.Sprintf(`{"version":1,"brands":[{"repo":"example/b","tag":"v1","commit":"x","dir":"vendor/brand/b","lint":"vendor/brand/b/export/lint.json","files":{"export/lint.json":"%s"}}]}`, hex.EncodeToString(sum[:]))
	os.WriteFile(filepath.Join(project, "brandkit.lock"), []byte(lock), 0o644)
	return project
}

func write(t *testing.T, p, s string) string {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func hookContext(t *testing.T, cwd, file string, extra ...string) string {
	t.Helper()
	in, _ := json.Marshal(map[string]any{"cwd": cwd, "tool_name": "Edit", "tool_input": map[string]any{"file_path": file}})
	code, out, _ := runCLI(t, string(in), append([]string{"lint", "--hook"}, extra...)...)
	if code != 0 {
		t.Fatalf("hook exit %d", code)
	}
	if out == "" {
		return ""
	}
	var msg struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &msg); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return msg.HookSpecificOutput.AdditionalContext
}

const brandPage = `<div class="shadow-md font-[Papyrus] bg-blue-500"><.icon name="hero-x-mark" /></div>` + "\n"

// TestHookFindsTheProjectFromTheFile: the brand comes from the nearest
// brandkit.lock above the edited file, wherever the session's cwd is (R1-M2).
func TestHookFindsTheProjectFromTheFile(t *testing.T) {
	project := brandProject(t)
	page := write(t, filepath.Join(project, "lib", "web", "page.heex"), brandPage)
	for _, cwd := range []string{project, filepath.Dir(project), filepath.Join(project, "lib"), t.TempDir()} {
		msg := hookContext(t, cwd, page)
		for _, rule := range []string{"R3", "R6", "R7", "R9"} {
			if !strings.Contains(msg, rule) {
				t.Errorf("cwd %s: no %s in %q", cwd, rule, msg)
			}
		}
		if strings.Contains(msg, "No brandkit.lock") {
			t.Errorf("cwd %s: %q", cwd, msg)
		}
	}
	// Without a lock the agent is told the brand rules were off.
	loose := write(t, filepath.Join(t.TempDir(), "page.heex"), brandPage)
	if msg := hookContext(t, filepath.Dir(loose), loose); !strings.Contains(msg, "No brandkit.lock") || strings.Contains(msg, "error R7") {
		t.Errorf("no lock: %q", msg)
	}
	// Files in folders lint skips are skipped here too (R1-m2).
	if msg := hookContext(t, project, write(t, filepath.Join(project, "testdata", "x.heex"), brandPage)); msg != "" {
		t.Errorf("testdata: %q", msg)
	}
}

// TestHookSaysWhyLintCantRun: a lock whose lint.json is missing, or a lock
// that doesn't parse, is reported to the agent instead of silence (R1-M3).
func TestHookSaysWhyLintCantRun(t *testing.T) {
	project := brandProject(t)
	page := write(t, filepath.Join(project, "page.heex"), brandPage)
	os.RemoveAll(filepath.Join(project, "vendor"))
	if msg := hookContext(t, project, page); !strings.Contains(msg, "couldn't check") || !strings.Contains(msg, "brandkit install") {
		t.Errorf("missing lint.json: %q", msg)
	}
	os.WriteFile(filepath.Join(project, "brandkit.lock"), []byte("<<<<<<< HEAD\n"), 0o644)
	if msg := hookContext(t, project, page); !strings.Contains(msg, "couldn't check") || !strings.Contains(msg, "brandkit.lock") {
		t.Errorf("broken lock: %q", msg)
	}
}

// TestLintFilesAndBrand: lint names what it checked and skipped, and exits
// 2 when it checked nothing or found no brand (unless --no-brand).
func TestLintFilesAndBrand(t *testing.T) {
	td, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "lint"))
	empty := t.TempDir()
	t.Chdir(empty)
	pass := filepath.Join(td, "R2", "pass.html")
	if code, _, errs := runCLI(t, "", "lint", pass); code != 2 || !strings.Contains(errs, "no brand") {
		t.Errorf("no brand: %d %s", code, errs)
	}
	if code, out, errs := runCLI(t, "", "lint", "--no-brand", pass); code != 0 || !strings.Contains(out, "1 files checked") || !strings.Contains(out, "no brand") {
		t.Errorf("--no-brand: %d %s %s", code, out, errs)
	}
	write(t, filepath.Join(empty, "main.go"), "package main\n")
	if code, out, errs := runCLI(t, "", "lint", "--no-brand", "main.go"); code != 2 || !strings.Contains(out, "skipped main.go") || !strings.Contains(errs, "no files checked") {
		t.Errorf("only unsupported files: %d %q %q", code, out, errs)
	}
	if code, _, errs := runCLI(t, "", "lint", "--no-brand"); code != 2 || !strings.Contains(errs, "no files checked") {
		t.Errorf("empty folder: %d %q", code, errs)
	}

	project := brandProject(t)
	write(t, filepath.Join(project, "assets", "app.css"), ".a { color: var(--color-base-content); }\n")
	write(t, filepath.Join(project, "assets", "app.js"), "")
	t.Chdir(filepath.Join(project, "assets")) // the lock is found above
	code, out, errs := runCLI(t, "", "lint")
	if code != 0 || !strings.Contains(out, "1 files checked") || !strings.Contains(out, "Fixture Base") ||
		!strings.Contains(out, "skipped 1 files of types lint doesn't read") || strings.Contains(out, "checked app.css") {
		t.Errorf("project: %d %q %q", code, out, errs)
	}
	if _, out, _ := runCLI(t, "", "lint", "-v"); !strings.Contains(out, "checked app.css") || !strings.Contains(out, "skipped app.js") {
		t.Errorf("-v: %q", out)
	}
}

// TestInstallCheck: install --check compares the vendored files with the
// lock offline (R1-M12).
func TestInstallCheck(t *testing.T) {
	project := brandProject(t)
	t.Chdir(project)
	if code, out, errs := runCLI(t, "", "install", "--check"); code != 0 || !strings.Contains(out, "1 brands match") {
		t.Fatalf("clean: %d %q %q", code, out, errs)
	}
	os.WriteFile(filepath.Join(project, "vendor", "brand", "b", "export", "lint.json"), []byte("{}"), 0o644)
	if code, out, _ := runCLI(t, "", "install", "--check"); code != 1 || !strings.Contains(out, "export/lint.json was edited") {
		t.Fatalf("edited: %d %q", code, out)
	}
	os.Remove(filepath.Join(project, "vendor", "brand", "b", "export", "lint.json"))
	if code, out, _ := runCLI(t, "", "install", "--check"); code != 1 || !strings.Contains(out, "export/lint.json is missing") {
		t.Fatalf("missing: %d %q", code, out)
	}
	t.Chdir(t.TempDir())
	if code, _, _ := runCLI(t, "", "install", "--check"); code != 2 {
		t.Fatalf("no lock: %d", code)
	}
}

// TestPluginHook runs the command in agent-plugin/hooks/hooks.json: with
// brandkit on PATH it warns; through mise it looks brandkit up from the
// project without installing it; without either it says so when the
// project has a lock, and stays silent otherwise.
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
	// The tools the hook needs, and nothing else.
	tools := t.TempDir()
	for _, name := range []string{"sh", "sed", "cat", "dirname"} {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Skip(err)
		}
		os.Symlink(p, filepath.Join(tools, name))
	}
	project := brandProject(t)
	page := write(t, filepath.Join(project, "templates", "page.gohtml"), brandPage)

	runHook := func(path, file string) string {
		in, _ := json.Marshal(map[string]any{"cwd": t.TempDir(), "tool_input": map[string]any{"file_path": file}})
		c := exec.Command(filepath.Join(tools, "sh"), cmdSpec.Args...)
		c.Env = []string{"PATH=" + path, "HOME=" + t.TempDir()}
		c.Stdin = bytes.NewReader(in)
		out, _ := c.Output()
		if code := c.ProcessState.ExitCode(); code != 0 {
			t.Fatalf("exit %d", code)
		}
		return string(out)
	}
	if out := runHook(bin+":"+tools, page); !strings.Contains(out, "R3") || !strings.Contains(out, "R7") {
		t.Errorf("with brandkit: %q", out)
	}
	if out := runHook(tools, page); !strings.Contains(out, "brandkit is not installed") {
		t.Errorf("without brandkit, with a lock: %q", out)
	}
	loose := write(t, filepath.Join(t.TempDir(), "page.gohtml"), brandPage)
	if out := runHook(tools, loose); out != "" {
		t.Errorf("without brandkit or a lock: %q", out)
	}
	if out := runHook(tools, write(t, filepath.Join(project, "main.go"), "package main\n")); out != "" {
		t.Errorf("a file lint doesn't read: %q", out)
	}
	// A fake mise: it answers `which` from the project folder only.
	miseBin := t.TempDir()
	log := filepath.Join(miseBin, "log")
	write(t, filepath.Join(miseBin, "mise"), fmt.Sprintf("#!%s\necho \"$*\" >> %s\n[ \"$1 $2 $3 $4\" = \"-C %s which brandkit\" ] && echo %s\n",
		filepath.Join(tools, "sh"), log, project, filepath.Join(bin, "brandkit")))
	os.Chmod(filepath.Join(miseBin, "mise"), 0o755)
	if out := runHook(miseBin+":"+tools, page); !strings.Contains(out, "R7") {
		t.Errorf("through mise: %q", out)
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "exec") || strings.Contains(string(calls), "install") {
		t.Errorf("the hook asked mise to install or exec: %q", calls)
	}
}
