package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarball packs a folder the way GitHub does: one top-level folder.
func tarball(t *testing.T, dir, top string, edit func(rel string, data []byte) []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		data, _ := os.ReadFile(p)
		if edit != nil {
			data = edit(rel, data)
		}
		tw.WriteHeader(&tar.Header{Name: top + "/" + rel, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

type fakeGitHub struct {
	tags     map[string]string // tag → commit
	tarballs map[string][]byte // commit → archive
	latest   string
	auth     string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.auth = r.Header.Get("Authorization")
	const prefix = "/repos/example/fixture-base-brand/"
	p, ok := strings.CutPrefix(r.URL.Path, prefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasPrefix(p, "commits/"):
		c, ok := f.tags[strings.TrimPrefix(p, "commits/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(c))
	case strings.HasPrefix(p, "tarball/"):
		b, ok := f.tarballs[strings.TrimPrefix(p, "tarball/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	case p == "releases/latest":
		json.NewEncoder(w).Encode(map[string]string{"tag_name": f.latest})
	default:
		http.NotFound(w, r)
	}
}

func TestInstallUpdateVerify(t *testing.T) {
	pkg := filepath.Join("..", "..", "testdata", "brands", "base")
	fake := &fakeGitHub{
		tags: map[string]string{"v1.0.0": "aaaa1111", "v1.1.0": "bbbb2222"},
		tarballs: map[string][]byte{
			"aaaa1111": tarball(t, pkg, "example-fixture-base-brand-aaaa1111", nil),
			"bbbb2222": tarball(t, pkg, "example-fixture-base-brand-bbbb2222", func(rel string, d []byte) []byte {
				if rel == "export/ui/tokens.css" {
					return append(d, []byte("/* v1.1.0 */\n")...)
				}
				return d
			}),
		},
		latest: "v1.1.0",
	}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := &Client{API: srv.URL, Token: "t0ken", HTTP: srv.Client()}
	project := t.TempDir()

	e, err := c.Install(project, "example/fixture-base-brand", "v1.0.0", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fake.auth != "Bearer t0ken" {
		t.Errorf("auth header %q", fake.auth)
	}
	for _, f := range []string{"brandkit.json", "export/ui/tokens.css", "export/ui/brand.css", "export/ui/daisyui-themes.css",
		"export/ui/daisyui-plugin.css", "export/lint.json", "tokens/base.resolver.json", "fonts/fonts.css", "assets/lockup-day.svg"} {
		if _, ok := e.Files[f]; !ok {
			t.Errorf("not vendored: %s", f)
		}
		if _, err := os.Stat(filepath.Join(project, "vendor", "brand", "fixture-base-brand", f)); err != nil {
			t.Error(err)
		}
	}
	if _, ok := e.Files["README.md"]; ok {
		t.Error("the README isn't part of an install")
	}
	l, err := ReadLock(project)
	if err != nil || len(l.Brands) != 1 || l.Brands[0].Commit != "aaaa1111" || l.Brands[0].Lint != "vendor/brand/fixture-base-brand/export/lint.json" {
		t.Fatalf("lock %+v %v", l, err)
	}
	if err := Verify(project); err != nil {
		t.Fatal(err)
	}

	// Edited vendored files are caught.
	tp := filepath.Join(project, "vendor", "brand", "fixture-base-brand", "export", "ui", "tokens.css")
	os.WriteFile(tp, []byte("tampered"), 0o644)
	if err := Verify(project); err == nil || !strings.Contains(err.Error(), "export/ui/tokens.css was edited") {
		t.Fatalf("Verify: %v", err)
	}
	// Reinstalling from the lock restores them.
	if _, err := c.Install(project, e.Repo, e.Tag, "vendor/brand", &l.Brands[0]); err != nil {
		t.Fatal(err)
	}
	if err := Verify(project); err != nil {
		t.Fatal(err)
	}

	// A tag that moved is refused.
	fake.tags["v1.0.0"] = "cccc3333"
	if _, err := c.Install(project, e.Repo, e.Tag, "vendor/brand", &l.Brands[0]); err == nil || !strings.Contains(err.Error(), "now points at") {
		t.Fatalf("moved tag: %v", err)
	}

	// Update to the newest release.
	tag, err := c.LatestTag(e.Repo)
	if err != nil || tag != "v1.1.0" {
		t.Fatalf("latest %q %v", tag, err)
	}
	if _, err := c.Install(project, e.Repo, tag, "vendor/brand", nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(tp)
	if !strings.HasSuffix(string(data), "/* v1.1.0 */\n") {
		t.Error("update didn't vendor v1.1.0")
	}
	l, _ = ReadLock(project)
	if len(l.Brands) != 1 || l.Brands[0].Tag != "v1.1.0" {
		t.Errorf("lock %+v", l)
	}
}

func TestUntarRefusesEscapes(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "top/../../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	if err := untar(&buf, t.TempDir()); err == nil {
		t.Fatal("want an error for a path leaving the folder")
	}
}

func TestParseRef(t *testing.T) {
	if r, tag, err := ParseRef("Applesauce-Labs/rosearch-brand@v1.2.0"); err != nil || r != "Applesauce-Labs/rosearch-brand" || tag != "v1.2.0" {
		t.Fatal(r, tag, err)
	}
	for _, bad := range []string{"nope", "a/b/c@v1", "../x@v1"} {
		if _, _, err := ParseRef(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
