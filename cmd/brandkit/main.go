// Command brandkit builds, checks and installs brand packages (DTCG design
// tokens), lints UI sources against them, and captures rendered pages.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/build"
	"github.com/shermanhuman/brandkit/internal/capture"
	"github.com/shermanhuman/brandkit/internal/check"
	"github.com/shermanhuman/brandkit/internal/install"
	"github.com/shermanhuman/brandkit/internal/lint"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.2.0"

const usage = `brandkit — brand packages from DTCG tokens

Brand packages (run in the package, or pass its folder):
  build [dir] [--no-images] [--chrome PATH]
                    write tokens.css, brand.css, both DaisyUI theme forms,
                    lint.json and the README brand sheet (PNG specimens need Chrome)
  check [dir] [--json]
                    schema, contrast of every declared pair, brand rules

Projects:
  install [owner/repo@tag] [--dir vendor/brand]
                    vendor a brand's built files and brand sheet and pin them in
                    brandkit.lock; with no argument, reinstall what the lock pins
  install --check   offline: exit 1 if a vendored file is missing or was edited
  update [owner/repo[@tag]]
                    move one brand (or all) to a tag (default: the newest release)
  lint [paths...] [--config lint.json] [--compiled app.css]... [--json] [-v]
       [--no-brand]
                    rules R1–R10 on CSS, HTML, HEEx, ~H and Go templates (default: .);
                    the config defaults to the lint.json of the brand in the nearest
                    brandkit.lock; without a brand it exits 2 unless --no-brand.
                    -v lists every file checked and skipped
  lint --hook       edit-time hook: reads the hook JSON on stdin, warns, never fails
  capture (--url URL | --cdp WS [--match TEXT] [--url URL]) [--out DIR]
          [--widths 375,768,1280,1920] [--height 900] [--chrome PATH]
          [--no-freeze] [--no-fail]
                    screenshots, axe-core, computed contrast and overflow per width;
                    writes DIR/report.json (default DIR: capture)

  version           print the version

Exit status: 0 clean; 1 findings (check, lint, capture, install --check);
2 a usage or runtime error, including lint with no files to check or no
brand, and capture of a page that answers 4xx/5xx. A brand package that
doesn't load is a check finding (1). capture --no-fail exits 0 on findings.
lint --hook always exits 0.
`

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var err error
	code := 0
	switch cmd {
	case "build":
		err = cmdBuild(rest, stdout)
	case "check":
		code, err = cmdCheck(rest, stdout)
	case "lint":
		code, err = cmdLint(rest, stdin, stdout)
	case "capture":
		code, err = cmdCapture(rest, stdout)
	case "install":
		code, err = cmdInstall(rest, stdout)
	case "update":
		err = cmdUpdate(rest, stdout)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "brandkit", version)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "brandkit: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "brandkit "+cmd+":", err)
		return 2
	}
	return code
}

// parse parses flags that may come before or after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func oneDir(pos []string) (string, error) {
	switch len(pos) {
	case 0:
		return ".", nil
	case 1:
		return pos[0], nil
	}
	return "", fmt.Errorf("one package folder at most, got %v", pos)
}

func cmdBuild(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	noImages := fs.Bool("no-images", false, "")
	chrome := fs.String("chrome", "", "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	dir, err := oneDir(pos)
	if err != nil {
		return err
	}
	b, err := brand.Load(dir)
	if err != nil {
		return err
	}
	written, err := build.Write(b, build.Options{Images: !*noImages, Chrome: *chrome})
	if err != nil {
		return err
	}
	if len(written) == 0 {
		fmt.Fprintln(out, "up to date")
	}
	for _, w := range written {
		fmt.Fprintln(out, "wrote", w)
	}
	return nil
}

func cmdCheck(args []string, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return 0, err
	}
	dir, err := oneDir(pos)
	if err != nil {
		return 0, err
	}
	b, err := brand.Load(dir)
	if err != nil {
		rule := "schema"
		var todo *brand.TodoError
		if errors.As(err, &todo) {
			rule = "todo"
		}
		if *asJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			enc.Encode([]check.Finding{{Rule: rule, Severity: "error", Message: err.Error()}})
		} else {
			fmt.Fprintf(out, "error %s: %v\n", rule, err)
		}
		return 1, nil
	}
	fs2 := check.Brand(b)
	if *asJSON {
		if fs2 == nil {
			fs2 = []check.Finding{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(fs2); err != nil {
			return 0, err
		}
	} else {
		for _, f := range fs2 {
			fmt.Fprintln(out, f)
		}
		fmt.Fprintf(out, "%s: %d themes, %d errors, %d warnings\n", b.Config.Name, len(b.Themes), check.Errors(fs2), len(fs2)-check.Errors(fs2))
	}
	if check.Errors(fs2) > 0 {
		return 1, nil
	}
	return 0, nil
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

// findProject walks up from dir to the nearest folder holding brandkit.lock.
func findProject(dir string) (string, bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, install.LockFile)); err == nil && st.Mode().IsRegular() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// lintConfig finds lint.json: the flag, else the first brand in the
// project's lock. brand reports whether a brand's rules were found. The
// lock's file hashes go into the config, so unedited copies of vendored
// files are skipped. project is "" when there is no lock.
func lintConfig(project, explicit string) (cfg *lint.Config, brand bool, err error) {
	var l *install.Lock
	if project != "" {
		if l, err = install.ReadLock(project); err != nil {
			return nil, false, err
		}
	}
	switch {
	case explicit != "":
		cfg, err = lint.LoadConfig(explicit)
		brand = true
	case l != nil && len(l.Brands) > 0:
		e := l.Brands[0]
		cfg, err = lint.LoadConfig(filepath.Join(project, filepath.FromSlash(e.Lint)))
		if err != nil {
			err = fmt.Errorf("the lint.json of %s (from %s): %w", e.Repo, install.LockFile, err)
		}
		brand = true
	default:
		cfg = lint.DefaultConfig()
	}
	if err != nil {
		return nil, false, err
	}
	if l != nil {
		cfg.Vendored = map[string]bool{}
		for _, e := range l.Brands {
			for _, h := range e.Files {
				cfg.Vendored[h] = true
			}
		}
	}
	return cfg, brand, nil
}

func cmdLint(args []string, stdin io.Reader, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "")
	asJSON := fs.Bool("json", false, "")
	hook := fs.Bool("hook", false, "")
	noBrand := fs.Bool("no-brand", false, "")
	verbose := fs.Bool("v", false, "")
	fs.BoolVar(verbose, "verbose", false, "")
	var compiled multi
	fs.Var(&compiled, "compiled", "")
	pos, err := parse(fs, args)
	if err != nil {
		return 0, err
	}
	if *hook {
		runHook(stdin, out, *cfgPath)
		return 0, nil
	}
	project, _ := findProject(".")
	cfg, hasBrand, err := lintConfig(project, *cfgPath)
	if err != nil {
		return 0, fmt.Errorf("%w; run brandkit install to restore the vendored brand files", err)
	}
	if !hasBrand && !*noBrand {
		return 0, fmt.Errorf("no brand: no %s naming a brand in this folder or above it. Run brandkit install owner/repo@tag, pass --config path/to/lint.json, or pass --no-brand to run only the rules that need no brand (R1–R5, R8)", install.LockFile)
	}
	if len(pos) == 0 && len(compiled) == 0 {
		pos = []string{"."}
	}
	// R10's marketing page globs are relative to the project root.
	cfg.Root = project
	res, err := lint.Paths(cfg, pos, compiled)
	if err != nil {
		return 0, err
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return 0, err
		}
	} else {
		for _, f := range res.Findings {
			fmt.Fprintln(out, f)
		}
		other := 0
		for _, s := range res.Skipped {
			if *verbose || s.Explicit || s.Reason != lint.SkipUnsupported {
				fmt.Fprintf(out, "skipped %s: %s\n", s.Path, s.Reason)
			} else {
				other++
			}
		}
		if *verbose {
			for _, c := range res.Checked {
				fmt.Fprintln(out, "checked", c)
			}
		}
		if other > 0 {
			fmt.Fprintf(out, "skipped %d files of types lint doesn't read (-v lists them)\n", other)
		}
		brandName := cfg.Brand
		if !hasBrand {
			brandName = "no brand: R6, R7, R9, R10 off"
		}
		fmt.Fprintf(out, "%d files checked, %d errors, %d warnings, %d ignored (%s)\n", res.Files, res.Errors(), len(res.Findings)-res.Errors(), len(res.Ignored), brandName)
	}
	if res.Files == 0 {
		return 0, fmt.Errorf("no files checked: lint reads .css, .html, .heex, .eex, .ex, .exs and Go template files (%s)", strings.Join(pos, " "))
	}
	if res.Errors() > 0 {
		return 1, nil
	}
	return 0, nil
}

// runHook is the PostToolUse edit hook: it lints the edited file and hands
// any findings back to the agent as context. It never fails or blocks, but
// when lint can't run it says why.
func runHook(stdin io.Reader, out io.Writer, cfgPath string) {
	say := func(msg string) {
		json.NewEncoder(out).Encode(map[string]any{
			"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": msg},
		})
	}
	defer func() {
		if r := recover(); r != nil {
			say(fmt.Sprintf("brandkit lint couldn't check this edit (internal error: %v). Run brandkit lint on the file to see the problem.", r))
		}
	}()
	var in struct {
		Cwd       string `json:"cwd"`
		ToolInput struct {
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in); err != nil {
		return
	}
	p := in.ToolInput.FilePath
	if p == "" || lint.KindOf(p) == lint.KindNone {
		return
	}
	if !filepath.IsAbs(p) {
		cwd := in.Cwd
		if cwd == "" {
			cwd = "."
		}
		p = filepath.Join(cwd, p)
	}
	// The project is the nearest folder above the file with brandkit.lock;
	// without one, the session's folder if the file is in it.
	project, hasLock := findProject(filepath.Dir(p))
	root := project
	if !hasLock {
		root = filepath.Dir(p)
		if in.Cwd != "" {
			if r, err := filepath.Rel(in.Cwd, p); err == nil && !strings.HasPrefix(r, "..") {
				root = in.Cwd
			}
		}
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		rel = p
	}
	rel = filepath.ToSlash(rel)
	if lint.Skipped(rel) {
		return
	}
	cfg, hasBrand, err := lintConfig(project, cfgPath)
	if err != nil {
		say(fmt.Sprintf("brandkit lint couldn't check %s: %v. The brand rules are off until this is fixed; run `brandkit install` in %s to restore the brand's files.", rel, err, project))
		return
	}
	data, err := os.ReadFile(p)
	if err != nil || cfg.VendoredCopy(data) {
		return
	}
	found, _ := lint.Source(cfg, rel, data, lint.KindOf(p))
	if len(found) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "brandkit lint found %d raw design value(s) in %s. CI fails on errors; use the brand's tokens and theme roles, or add `brand-lint-ignore R#: reason` if the value is deliberate:\n", len(found), rel)
	for i, f := range found {
		if i == 20 {
			fmt.Fprintf(&b, "… and %d more (run brandkit lint %s)\n", len(found)-20, rel)
			break
		}
		fmt.Fprintf(&b, "- line %d: %s %s: %s (%s)\n", f.Line, f.Severity, f.Rule, f.Message, f.Text)
	}
	if !hasBrand {
		fmt.Fprintf(&b, "No %s was found above this file, so the brand rules (R6 fonts, R7 shadows, R9 icons, R10 corners) were off.\n", install.LockFile)
	}
	say(b.String())
}

func cmdCapture(args []string, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	var o capture.Options
	fs.StringVar(&o.URL, "url", "", "")
	fs.StringVar(&o.CDP, "cdp", "", "")
	fs.StringVar(&o.Match, "match", "", "")
	fs.StringVar(&o.Out, "out", "capture", "")
	fs.StringVar(&o.Chrome, "chrome", "", "")
	fs.IntVar(&o.Height, "height", 900, "")
	widths := fs.String("widths", "", "")
	noFail := fs.Bool("no-fail", false, "")
	fs.BoolVar(&o.NoFreeze, "no-freeze", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return 0, err
	}
	if len(pos) > 0 {
		return 0, fmt.Errorf("unexpected arguments %v", pos)
	}
	if *widths != "" {
		for _, w := range strings.Split(*widths, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(w))
			if err != nil || n < 200 || n > 4000 {
				return 0, fmt.Errorf("bad width %q", w)
			}
			o.Widths = append(o.Widths, n)
		}
	}
	rep, err := capture.Run(context.Background(), o)
	if err != nil {
		return 0, err
	}
	s := rep.Summary
	if rep.FinalURL != "" {
		fmt.Fprintf(out, "note: %s redirected to %s; the report is of that page\n", rep.URL, rep.FinalURL)
	}
	fmt.Fprintf(out, "%s: axe serious/critical %d, other %d; contrast failures %d; overflow at %v\nreport: %s\n",
		rep.URL, s.AxeSeriousCritical, s.AxeOther, s.ContrastFailures, s.OverflowWidths, filepath.Join(o.Out, "report.json"))
	if s.Failed() && !*noFail {
		return 1, nil
	}
	return 0, nil
}

func cmdInstall(args []string, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	dir := fs.String("dir", install.DefaultDir, "")
	checkOnly := fs.Bool("check", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return 0, err
	}
	if *checkOnly {
		if len(pos) > 0 {
			return 0, fmt.Errorf("--check takes no arguments; it checks what %s pins", install.LockFile)
		}
		n, err := install.Verify(".")
		var ve *install.VerifyError
		if errors.As(err, &ve) {
			for _, p := range ve.Problems {
				fmt.Fprintln(out, p)
			}
			fmt.Fprintf(out, "%d vendored files differ from %s; run brandkit install to restore them\n", len(ve.Problems), install.LockFile)
			return 1, nil
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, fmt.Errorf("no brands in %s to check", install.LockFile)
		}
		fmt.Fprintf(out, "%d brands match %s\n", n, install.LockFile)
		return 0, nil
	}
	c := install.NewClient()
	if len(pos) == 0 {
		l, err := install.ReadLock(".")
		if err != nil {
			return 0, err
		}
		if len(l.Brands) == 0 {
			return 0, fmt.Errorf("nothing in %s; give owner/repo@tag", install.LockFile)
		}
		var ve *install.VerifyError
		if _, err := install.Verify("."); errors.As(err, &ve) {
			for _, p := range ve.Problems {
				fmt.Fprintln(out, "restoring:", p)
			}
		}
		for _, e := range l.Brands {
			if _, err := c.Install(".", e.Repo, e.Tag, filepath.Dir(e.Dir), &e); err != nil {
				return 0, err
			}
			fmt.Fprintf(out, "%s@%s verified in %s\n", e.Repo, e.Tag, e.Dir)
		}
		return 0, nil
	}
	for _, ref := range pos {
		repo, tag, err := install.ParseRef(ref)
		if err != nil {
			return 0, err
		}
		if tag == "" {
			return 0, fmt.Errorf("%s: pin a tag (owner/repo@tag), or use brandkit update", ref)
		}
		e, err := c.Install(".", repo, tag, *dir, nil)
		if err != nil {
			return 0, err
		}
		fmt.Fprintf(out, "installed %s@%s (%s) into %s: %d files\n", repo, tag, e.Commit[:min(12, len(e.Commit))], e.Dir, len(e.Files))
	}
	return 0, nil
}

func cmdUpdate(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	l, err := install.ReadLock(".")
	if err != nil {
		return err
	}
	c := install.NewClient()
	targets := map[string]string{}
	if len(pos) == 0 {
		for _, e := range l.Brands {
			targets[e.Repo] = ""
		}
	}
	for _, ref := range pos {
		repo, tag, err := install.ParseRef(ref)
		if err != nil {
			return err
		}
		targets[repo] = tag
	}
	if len(targets) == 0 {
		return fmt.Errorf("nothing in %s to update", install.LockFile)
	}
	for _, e := range l.Brands {
		tag, ok := targets[e.Repo]
		if !ok {
			continue
		}
		delete(targets, e.Repo)
		if tag == "" {
			if tag, err = c.LatestTag(e.Repo); err != nil {
				return err
			}
		}
		if tag == e.Tag {
			fmt.Fprintf(out, "%s is at %s already\n", e.Repo, tag)
			continue
		}
		if _, err := c.Install(".", e.Repo, tag, filepath.Dir(e.Dir), nil); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: %s → %s\n", e.Repo, e.Tag, tag)
	}
	for repo := range targets {
		return fmt.Errorf("%s isn't installed; use brandkit install %s@<tag>", repo, repo)
	}
	return nil
}
