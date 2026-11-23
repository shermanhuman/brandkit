package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"maps"
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
	// Any owner serves the same package, so two owners can share a name.
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/repos/"), "/", 3)
	if len(parts) != 3 || parts[1] != "fixture-base-brand" {
		http.NotFound(w, r)
		return
	}
	p := parts[2]
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
	// The brand sheet comes too: the README and its images.
	for _, f := range []string{"README.md", "export/sheet/sw-00696f.svg", "export/sheet/type-day.png"} {
		if _, ok := e.Files[f]; !ok {
			t.Errorf("brand sheet not vendored: %s", f)
		}
	}
	l, err := ReadLock(project)
	if err != nil || len(l.Brands) != 1 || l.Brands[0].Commit != "aaaa1111" || l.Brands[0].Lint != "vendor/brand/fixture-base-brand/export/lint.json" {
		t.Fatalf("lock %+v %v", l, err)
	}
	if n, err := Verify(project); err != nil || n != 1 {
		t.Fatal(n, err)
	}

	// Edited and missing vendored files are caught.
	tp := filepath.Join(project, "vendor", "brand", "fixture-base-brand", "export", "ui", "tokens.css")
	os.WriteFile(tp, []byte("tampered"), 0o644)
	os.Remove(filepath.Join(project, "vendor", "brand", "fixture-base-brand", "README.md"))
	if _, err := Verify(project); err == nil || !strings.Contains(err.Error(), "export/ui/tokens.css was edited") || !strings.Contains(err.Error(), "README.md is missing") {
		t.Fatalf("Verify: %v", err)
	}
	// Reinstalling from the lock restores them.
	if _, err := c.Install(project, e.Repo, e.Tag, "vendor/brand", &l.Brands[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(project); err != nil {
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

// TestInstallRefusesPathsLeavingThePackage: a package's brandkit.json can't
// point install at files outside the package (R1-M1).
func TestInstallRefusesPathsLeavingThePackage(t *testing.T) {
	pkg := filepath.Join("..", "..", "testdata", "brands", "base")
	secretDir := t.TempDir()
	os.WriteFile(filepath.Join(secretDir, "secret.txt"), []byte("key"), 0o644)
	for key, val := range map[string]any{
		"lint":      "../../../../../../../../../../.." + filepath.ToSlash(secretDir) + "/secret.txt",
		"readme":    map[string]any{"file": "../outside.md"},
		"install":   []any{"../"},
		"tokensCss": map[string]any{"file": "../../x.css"},
	} {
		field := key
		fake := &fakeGitHub{
			tags: map[string]string{"v1": "aaaa1111"},
			tarballs: map[string][]byte{"aaaa1111": tarball(t, pkg, "top", func(rel string, d []byte) []byte {
				if rel != "brandkit.json" {
					return d
				}
				var m map[string]any
				if err := json.Unmarshal(d, &m); err != nil {
					t.Fatal(err)
				}
				m[key] = val
				out, _ := json.Marshal(m)
				return out
			})},
		}
		srv := httptest.NewServer(fake)
		c := &Client{API: srv.URL, HTTP: srv.Client()}
		project := t.TempDir()
		_, err := c.Install(project, "example/fixture-base-brand", "v1", "", nil)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "leaves its folder") {
			t.Errorf("%s: %v", field, err)
		}
		if _, err := os.Stat(filepath.Join(project, LockFile)); err == nil {
			t.Errorf("%s: a lock was written", field)
		}
	}
}

// TestLockPathsChecked: a lock whose dir, lint or files leave the project
// is refused before anything reads or removes it (R1-M1).
func TestLockPathsChecked(t *testing.T) {
	for _, entry := range []string{
		`{"repo":"a/b","tag":"v1","commit":"c","dir":"../../home","lint":"x/lint.json","files":{}}`,
		`{"repo":"a/b","tag":"v1","commit":"c","dir":"/","lint":"x/lint.json","files":{}}`,
		`{"repo":"a/b","tag":"v1","commit":"c","dir":"vendor/brand/b","lint":"../../.ssh/id_rsa","files":{}}`,
		`{"repo":"a/b","tag":"v1","commit":"c","dir":"vendor/brand/b","lint":"x","files":{"../../x":"00"}}`,
		`{"repo":"../x","tag":"v1","commit":"c","dir":"vendor/brand/b","lint":"x","files":{}}`,
	} {
		project := t.TempDir()
		os.WriteFile(filepath.Join(project, LockFile), []byte(`{"version":1,"brands":[`+entry+`]}`), 0o644)
		if _, err := ReadLock(project); err == nil {
			t.Errorf("accepted %s", entry)
		}
	}
	if _, err := (&Client{}).Install(t.TempDir(), "a/b", "v1", "../up", nil); err == nil || !strings.Contains(err.Error(), "leaves its folder") {
		t.Errorf("--dir ../up: %v", err)
	}
}

func TestUntarLimits(t *testing.T) {
	pack := func(sizes ...int) *bytes.Buffer {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for i, n := range sizes {
			tw.WriteHeader(&tar.Header{Name: "top/f" + string(rune('a'+i)), Mode: 0o644, Size: int64(n), Typeflag: tar.TypeReg})
			tw.Write(bytes.Repeat([]byte("x"), n))
		}
		tw.Close()
		gz.Close()
		return &buf
	}
	if err := untarLimits(pack(10, 10), t.TempDir(), 10, 100); err != nil {
		t.Fatal(err)
	}
	if err := untarLimits(pack(11), t.TempDir(), 10, 100); err == nil {
		t.Error("a file over the limit was cut short without an error")
	}
	if err := untarLimits(pack(10, 10, 10), t.TempDir(), 10, 25); err == nil {
		t.Error("no total size cap")
	}
}

func newFake(t *testing.T) (*fakeGitHub, *Client, func()) {
	t.Helper()
	pkg := filepath.Join("..", "..", "testdata", "brands", "base")
	fake := &fakeGitHub{
		tags:     map[string]string{"v1": "aaaa1111"},
		tarballs: map[string][]byte{"aaaa1111": tarball(t, pkg, "top", nil)},
	}
	srv := httptest.NewServer(fake)
	return fake, &Client{API: srv.URL, HTTP: srv.Client()}, srv.Close
}

// snapshot reads every file under dir, keyed by its relative path.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			data, _ := os.ReadFile(p)
			m[rel] = string(data)
		}
		return nil
	})
	return m
}

// TestInstallRefusesSymlinks: a symlinked ancestor of the destination, or a
// symlinked destination, is refused before anything is removed or written,
// so the outside folder survives (A-M1).
func TestInstallRefusesSymlinks(t *testing.T) {
	_, c, done := newFake(t)
	defer done()
	for _, link := range []string{"vendor", "vendor/brand", "vendor/brand/fixture-base-brand"} {
		outside := t.TempDir()
		// The outside folder looks like whatever the link stands for.
		target := outside
		switch link {
		case "vendor":
			target = filepath.Join(outside, "brand", "fixture-base-brand")
		case "vendor/brand":
			target = filepath.Join(outside, "fixture-base-brand")
		}
		os.MkdirAll(target, 0o755)
		sentinel := filepath.Join(target, "brandkit.json")
		os.WriteFile(sentinel, []byte("outside"), 0o644)

		project := t.TempDir()
		lp := filepath.Join(project, filepath.FromSlash(link))
		os.MkdirAll(filepath.Dir(lp), 0o755)
		if err := os.Symlink(outside, lp); err != nil {
			t.Skip("no symlinks here:", err)
		}
		_, err := c.Install(project, "example/fixture-base-brand", "v1", "", nil)
		if err == nil || !strings.Contains(err.Error(), "is a symlink") {
			t.Errorf("%s symlinked: %v", link, err)
		}
		if data, err := os.ReadFile(sentinel); err != nil || string(data) != "outside" {
			t.Errorf("%s symlinked: the outside file was changed or removed (%q, %v)", link, data, err)
		}
		if got := snapshot(t, outside); len(got) != 1 {
			t.Errorf("%s symlinked: files written outside the project: %v", link, got)
		}
		if _, err := os.Stat(filepath.Join(project, LockFile)); err == nil {
			t.Errorf("%s symlinked: a lock was written", link)
		}
	}
}

// TestInstallRejectedBytesLeaveTheOldInstall: when a download for the
// pinned commit doesn't match the lock's hashes, the installed tree and the
// lock stay exactly as they were, and no staging folder is left (A-M2).
func TestInstallRejectedBytesLeaveTheOldInstall(t *testing.T) {
	fake, c, done := newFake(t)
	defer done()
	project := t.TempDir()
	if _, err := c.Install(project, "example/fixture-base-brand", "v1", "", nil); err != nil {
		t.Fatal(err)
	}
	vendor := filepath.Join(project, "vendor", "brand")
	tree := snapshot(t, vendor)
	lock, _ := os.ReadFile(filepath.Join(project, LockFile))
	l, err := ReadLock(project)
	if err != nil {
		t.Fatal(err)
	}

	// Same commit, changed bytes.
	pkg := filepath.Join("..", "..", "testdata", "brands", "base")
	fake.tarballs["aaaa1111"] = tarball(t, pkg, "top", func(rel string, d []byte) []byte {
		if rel == "export/ui/tokens.css" {
			return append(d, []byte("/* swapped */\n")...)
		}
		return d
	})
	_, err = c.Install(project, "example/fixture-base-brand", "v1", "vendor/brand", &l.Brands[0])
	if err == nil || !strings.Contains(err.Error(), "differs from the hash") {
		t.Fatalf("changed bytes: %v", err)
	}
	if got := snapshot(t, vendor); !maps.Equal(got, tree) {
		t.Error("the installed tree changed after the hash check failed")
	}
	if got, _ := os.ReadFile(filepath.Join(project, LockFile)); !bytes.Equal(got, lock) {
		t.Error("the lock changed after the hash check failed")
	}
	if n, err := Verify(project); err != nil || n != 1 {
		t.Errorf("Verify after a rejected install: %d %v", n, err)
	}
}

// TestInstallRefusesAnotherReposFolder: two owners' repositories with the
// same name can't share a folder; the second is told to pick --dir (A-M3).
func TestInstallRefusesAnotherReposFolder(t *testing.T) {
	_, c, done := newFake(t)
	defer done()
	project := t.TempDir()
	if _, err := c.Install(project, "acme/fixture-base-brand", "v1", "", nil); err != nil {
		t.Fatal(err)
	}
	tree := snapshot(t, filepath.Join(project, "vendor"))
	_, err := c.Install(project, "other/fixture-base-brand", "v1", "", nil)
	if err == nil || !strings.Contains(err.Error(), "acme/fixture-base-brand") || !strings.Contains(err.Error(), "--dir vendor/brand/other") {
		t.Fatalf("same folder, other owner: %v", err)
	}
	if got := snapshot(t, filepath.Join(project, "vendor")); !maps.Equal(got, tree) {
		t.Error("the first brand's files changed")
	}
	// Inside another brand's folder is refused too.
	if _, err := c.Install(project, "other/fixture-base-brand", "v1", "vendor/brand/fixture-base-brand", nil); err == nil {
		t.Error("installed inside another brand's folder")
	}
	// The suggested --dir works, and the lock holds both.
	if _, err := c.Install(project, "other/fixture-base-brand", "v1", "vendor/brand/other", nil); err != nil {
		t.Fatal(err)
	}
	if n, err := Verify(project); err != nil || n != 2 {
		t.Fatalf("Verify: %d %v", n, err)
	}
}
