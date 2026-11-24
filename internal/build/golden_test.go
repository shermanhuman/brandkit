package build

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/browser"
	"github.com/shermanhuman/brandkit/internal/install"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var fixtures = []string{"base", "sub"}

func fixtureDir(name string) string { return filepath.Join("..", "..", "testdata", "brands", name) }

func load(t *testing.T, name string) *brand.Brand {
	t.Helper()
	b, err := brand.Load(fixtureDir(name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func compare(t *testing.T, b *brand.Brand, files []File) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(b.Dir, filepath.FromSlash(f.Path))
		if *update {
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, f.Data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v (run go test ./internal/build -update)", f.Path, err)
			continue
		}
		if !bytes.Equal(want, f.Data) {
			t.Errorf("%s differs from the golden file (run go test ./internal/build -update and review the diff)", f.Path)
		}
	}
}

// TestGoldenText builds each fixture brand and compares every text output
// byte for byte with the files committed in testdata.
func TestGoldenText(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			b := load(t, name)
			files, err := Generate(b)
			if err != nil {
				t.Fatal(err)
			}
			compare(t, b, files)
			// Nothing stale: every file in the sheet folder is generated.
			gen := map[string]bool{}
			for _, f := range files {
				gen[f.Path] = true
			}
			for _, p := range ImagePaths(b) {
				gen[p] = true
			}
			entries, _ := os.ReadDir(filepath.Join(b.Dir, filepath.FromSlash(b.Config.Readme.Assets)))
			for _, e := range entries {
				if p := b.Config.Readme.Assets + "/" + e.Name(); !gen[p] {
					t.Errorf("%s is no longer generated; delete it", p)
				}
			}
		})
	}
}

// TestGoldenImages renders the PNG specimens with the pinned headless
// shell and compares them byte for byte. Fonts come from the fixture, so
// the pixels depend only on the browser build: Linux only.
func TestGoldenImages(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("PNG goldens are made on Linux")
	}
	if _, err := browser.Find(""); err != nil {
		if os.Getenv("BRANDKIT_REQUIRE_TOOLS") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			b := load(t, name)
			files, err := Render(b, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 2*len(b.Themes) {
				t.Fatalf("%d images", len(files))
			}
			compare(t, b, files)
		})
	}
}

// TestSubVendorsBase keeps the sub fixture's vendored base what
// `brandkit install` would write from the base fixture: every file it
// ships, byte for byte. After changing the base, copy them again:
// go test ./internal/build -run TestSubVendorsBase -update.
func TestSubVendorsBase(t *testing.T) {
	files, _, err := install.Files(fixtureDir("base"))
	if err != nil {
		t.Fatal(err)
	}
	vendor := filepath.Join(fixtureDir("sub"), "vendor", "fixture-base")
	want := map[string]bool{}
	for _, f := range files {
		want[f] = true
		a, _ := os.ReadFile(filepath.Join(fixtureDir("base"), filepath.FromSlash(f)))
		p := filepath.Join(vendor, filepath.FromSlash(f))
		if *update {
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(p, a, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		b, _ := os.ReadFile(p)
		if len(a) == 0 || !bytes.Equal(a, b) {
			t.Errorf("sub/vendor/fixture-base/%s differs from base (run go test ./internal/build -run TestSubVendorsBase -update)", f)
		}
	}
	filepath.WalkDir(vendor, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(vendor, p)
		if !want[filepath.ToSlash(rel)] {
			if *update {
				return os.Remove(p)
			}
			t.Errorf("sub/vendor/fixture-base/%s isn't something the base ships", rel)
		}
		return nil
	})
}

func TestExtendsOverrides(t *testing.T) {
	b := load(t, "sub")
	night := b.Theme("night")
	if got := night.Tokens.Get("color.primary").Alias; got != "palette.violet-300" {
		t.Errorf("sub night primary = %s, want the sub brand's violet", got)
	}
	if got := night.Tokens.Get("color.accent").Alias; got != "palette.coral-300" {
		t.Errorf("sub night accent = %s, want the base's coral", got)
	}
	r, err := night.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if r.Lockup["night"] != "assets/lockup-night.svg" || len(r.Contrast) == 0 || r.Shadows != "none" {
		t.Errorf("rules should merge the sub brand's lockup onto the base's rules: %+v", r)
	}
	if len(r.Do) != 1 {
		t.Errorf("a list in the sub brand replaces the base's: %v", r.Do)
	}
}

func TestTemplateTokensCSS(t *testing.T) {
	b := load(t, "sub")
	out, err := TokensCSS(b)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "/* Fixture Sub colours, in the legacy layout") || !strings.Contains(s, "\n[data-theme=\"light\"] {\n") {
		t.Errorf("template output:\n%s", s)
	}
}

// TestWriteRemovesStaleSheetFiles: a swatch for a colour the brand no
// longer has is removed; files brandkit didn't name are kept (R1-m9).
func TestWriteRemovesStaleSheetFiles(t *testing.T) {
	src := fixtureDir("base")
	dir := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	sheet := filepath.Join(dir, "export", "sheet")
	os.WriteFile(filepath.Join(sheet, "sw-123456.svg"), []byte("<svg/>"), 0o644)
	os.WriteFile(filepath.Join(sheet, "notes.txt"), []byte("mine"), 0o644)
	b, err := brand.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	written, err := Write(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sheet, "sw-123456.svg")); !os.IsNotExist(err) {
		t.Errorf("stale swatch kept: %v (wrote %v)", err, written)
	}
	for _, keep := range []string{"notes.txt", "sw-00696f.svg", "type-day.png"} {
		if _, err := os.Stat(filepath.Join(sheet, keep)); err != nil {
			t.Error(err)
		}
	}
}
