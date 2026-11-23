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
var version = "0.1.0"

const usage = `brandkit — brand packages from DTCG tokens

Brand packages (run in the package, or pass its folder):
  build [dir] [--no-images] [--chrome PATH]
                    write tokens.css, brand.css, both DaisyUI theme forms,
                    lint.json and the README brand sheet (PNG specimens need Chrome)
  check [dir] [--json]
                    schema, contrast of every declared pair, brand rules

Projects:
  install [owner/repo@tag] [--dir vendor/brand]
                    vendor a brand's built files and pin them in brandkit.lock;
                    with no argument, reinstall what the lock pins and verify it
  update [owner/repo[@tag]]
                    move one brand (or all) to a tag (default: the newest release)
  lint [paths...] [--config lint.json] [--compiled app.css]... [--json]
                    rules R1–R9 on CSS, HTML, HEEx, ~H and Go templates (default: .);
                    the config defaults to the lint.json of the brand in brandkit.lock
  lint --hook       edit-time hook: reads the hook JSON on stdin, warns, never fails
  capture (--url URL | --cdp WS [--match TEXT] [--url URL]) [--out DIR]
          [--widths 375,768,1280,1920] [--height 900] [--chrome PATH]
          [--no-freeze] [--no-fail]
                    screenshots, axe-core, computed contrast and overflow per width;
                    writes DIR/report.json (default DIR: capture)

  version           print the version

Exit status: 0 clean, 1 findings, 2 a usage or runtime error.
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
		err = cmdInstall(rest, stdout)
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
		if *asJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			enc.Encode([]check.Finding{{Rule: "schema", Severity: "error", Message: err.Error()}})
		} else {
			fmt.Fprintln(out, "error schema:", err)
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

// lintConfig finds lint.json: the flag, else the first brand in the lock.
func lintConfig(project, explicit string) (*lint.Config, string, error) {
	if explicit != "" {
		c, err := lint.LoadConfig(explicit)
		return c, explicit, err
	}
	l, err := install.ReadLock(project)
	if err != nil {
		return nil, "", err
	}
	for _, e := range l.Brands {
		p := filepath.Join(project, filepath.FromSlash(e.Lint))
		c, err := lint.LoadConfig(p)
		return c, p, err
	}
	return lint.DefaultConfig(), "", nil
}

func cmdLint(args []string, stdin io.Reader, out io.Writer) (int, error) {
	fs := flag.NewFlagSet("lint", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "")
	asJSON := fs.Bool("json", false, "")
	hook := fs.Bool("hook", false, "")
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
	cfg, _, err := lintConfig(".", *cfgPath)
	if err != nil {
		return 0, err
	}
	if len(pos) == 0 && len(compiled) == 0 {
		pos = []string{"."}
	}
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
		brandName := cfg.Brand
		if brandName == "" {
			brandName = "no brand (R6, R7, R9 off)"
		}
		fmt.Fprintf(out, "%d files, %d errors, %d warnings, %d ignored (%s)\n", res.Files, res.Errors(), len(res.Findings)-res.Errors(), len(res.Ignored), brandName)
	}
	if res.Errors() > 0 {
		return 1, nil
	}
	return 0, nil
}

// runHook is the PostToolUse edit hook: it lints the edited file and hands
// any findings back to the agent as context. It never fails or blocks.
func runHook(stdin io.Reader, out io.Writer, cfgPath string) {
	defer func() { recover() }()
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
	project := in.Cwd
	if project == "" {
		project = "."
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(project, p)
	}
	cfg, _, err := lintConfig(project, cfgPath)
	if err != nil {
		return
	}
	if slices(cfg.Generated, filepath.Base(p)) || strings.Contains(filepath.ToSlash(p), "/vendor/") {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(project, p)
	if err != nil {
		rel = p
	}
	found, _ := lint.Source(cfg, filepath.ToSlash(rel), data, lint.KindOf(p))
	if len(found) == 0 {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "brandkit lint found %d raw design value(s) in %s. CI fails on errors; use the brand's tokens and theme roles, or add `brand-lint-ignore R#: reason` if the value is deliberate:\n", len(found), filepath.ToSlash(rel))
	for i, f := range found {
		if i == 20 {
			fmt.Fprintf(&b, "… and %d more (run brandkit lint %s)\n", len(found)-20, filepath.ToSlash(rel))
			break
		}
		fmt.Fprintf(&b, "- line %d: %s %s: %s (%s)\n", f.Line, f.Severity, f.Rule, f.Message, f.Text)
	}
	json.NewEncoder(out).Encode(map[string]any{
		"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": b.String()},
	})
}

func slices(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
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
	fmt.Fprintf(out, "%s: axe serious/critical %d, other %d; contrast failures %d; overflow at %v\nreport: %s\n",
		rep.URL, s.AxeSeriousCritical, s.AxeOther, s.ContrastFailures, s.OverflowWidths, filepath.Join(o.Out, "report.json"))
	if s.Failed() && !*noFail {
		return 1, nil
	}
	return 0, nil
}

func cmdInstall(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	dir := fs.String("dir", install.DefaultDir, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	c := install.NewClient()
	if len(pos) == 0 {
		l, err := install.ReadLock(".")
		if err != nil {
			return err
		}
		if len(l.Brands) == 0 {
			return fmt.Errorf("nothing in %s; give owner/repo@tag", install.LockFile)
		}
		for _, e := range l.Brands {
			e := e
			if _, err := c.Install(".", e.Repo, e.Tag, filepath.Dir(e.Dir), &e); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s@%s verified in %s\n", e.Repo, e.Tag, e.Dir)
		}
		return nil
	}
	for _, ref := range pos {
		repo, tag, err := install.ParseRef(ref)
		if err != nil {
			return err
		}
		if tag == "" {
			return fmt.Errorf("%s: pin a tag (owner/repo@tag), or use brandkit update", ref)
		}
		e, err := c.Install(".", repo, tag, *dir, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "installed %s@%s (%s) into %s: %d files\n", repo, tag, e.Commit[:min(12, len(e.Commit))], e.Dir, len(e.Files))
	}
	return nil
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
