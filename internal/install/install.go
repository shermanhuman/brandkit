// Package install vendors a brand package's built files into a project at
// a pinned tag and records them in brandkit.lock.
package install

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/shermanhuman/brandkit/internal/brand"
)

// LockFile is the lock file's name, at the project root.
const LockFile = "brandkit.lock"

// DefaultDir is where brands are vendored, one folder per repository.
const DefaultDir = "vendor/brand"

// Lock is brandkit.lock.
type Lock struct {
	Version int     `json:"version"`
	Brands  []Entry `json:"brands"`
}

// Entry is one installed brand.
type Entry struct {
	Repo   string `json:"repo"`
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
	Dir    string `json:"dir"`
	// Lint is the installed lint.json, relative to the project.
	Lint  string            `json:"lint"`
	Files map[string]string `json:"files"` // path within Dir → sha256
}

// Client fetches from GitHub.
type Client struct {
	API   string // default https://api.github.com, or $BRANDKIT_GITHUB_API
	Token string
	HTTP  *http.Client
}

// NewClient uses $GH_TOKEN, $GITHUB_TOKEN or `gh auth token` for private repositories.
func NewClient() *Client {
	c := &Client{API: os.Getenv("BRANDKIT_GITHUB_API"), HTTP: &http.Client{Timeout: 2 * time.Minute}}
	if c.API == "" {
		c.API = "https://api.github.com"
	}
	c.Token = os.Getenv("GH_TOKEN")
	if c.Token == "" {
		c.Token = os.Getenv("GITHUB_TOKEN")
	}
	if c.Token == "" {
		if gh, err := exec.LookPath("gh"); err == nil {
			if out, err := exec.Command(gh, "auth", "token").Output(); err == nil {
				c.Token = strings.TrimSpace(string(out))
			}
		}
	}
	return c
}

func (c *Client) get(p, accept string) (*http.Response, error) {
	req, err := http.NewRequest("GET", strings.TrimSuffix(c.API, "/")+p, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s: %s", p, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// ParseRef splits owner/repo@tag.
func ParseRef(s string) (repo, tag string, err error) {
	repo, tag, _ = strings.Cut(s, "@")
	owner, name, _ := strings.Cut(repo, "/")
	if !repoRe.MatchString(repo) || strings.Trim(owner, ".") == "" || strings.Trim(name, ".") == "" {
		return "", "", fmt.Errorf("%q: want owner/repo@tag", s)
	}
	return repo, tag, nil
}

// Commit resolves a tag to its commit.
func (c *Client) Commit(repo, tag string) (string, error) {
	resp, err := c.get("/repos/"+repo+"/commits/"+tag, "application/vnd.github.sha")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 100))
	return strings.TrimSpace(string(b)), err
}

// LatestTag is the newest release's tag, or the newest tag when the
// repository has no releases.
func (c *Client) LatestTag(repo string) (string, error) {
	if resp, err := c.get("/repos/"+repo+"/releases/latest", "application/vnd.github+json"); err == nil {
		defer resp.Body.Close()
		var r struct {
			TagName string `json:"tag_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&r); err == nil && r.TagName != "" {
			return r.TagName, nil
		}
	}
	resp, err := c.get("/repos/"+repo+"/tags?per_page=1", "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("%s has no tags", repo)
	}
	return tags[0].Name, nil
}

// fetch downloads the repository at a commit into a temporary folder.
func (c *Client) fetch(repo, commit string) (string, error) {
	resp, err := c.get("/repos/"+repo+"/tarball/"+commit, "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	dir, err := os.MkdirTemp("", "brandkit-install-")
	if err != nil {
		return "", err
	}
	if err := untar(resp.Body, dir); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// Size limits for a brand archive.
const (
	maxFileSize  = 200 << 20
	maxTotalSize = 1 << 30
)

// untar extracts regular files, dropping the archive's top folder.
func untar(r io.Reader, dir string) error {
	return untarLimits(r, dir, maxFileSize, maxTotalSize)
}

func untarLimits(r io.Reader, dir string, maxFile, maxTotal int64) error {
	var total int64
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if slices.Contains(strings.Split(h.Name, "/"), "..") || path.IsAbs(h.Name) {
			return fmt.Errorf("unsafe path %q in the archive", h.Name)
		}
		name := path.Clean(h.Name)
		if _, rest, ok := strings.Cut(name, "/"); ok {
			name = rest
		} else {
			continue
		}
		if name == "" || strings.HasPrefix(name, "../") || path.IsAbs(name) || name == ".." {
			return fmt.Errorf("unsafe path %q in the archive", h.Name)
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, io.LimitReader(tr, maxFile+1))
		if err == nil && n > maxFile {
			err = fmt.Errorf("%s in the archive is over %d MiB", name, maxFile>>20)
		}
		if total += n; err == nil && total > maxTotal {
			err = fmt.Errorf("the archive holds over %d MiB", maxTotal>>20)
		}
		if err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
}

// SafeRel cleans a slash-separated relative path and rejects one that is
// absolute or leaves its folder. Every path that comes from a package's
// brandkit.json or from brandkit.lock goes through it.
func SafeRel(p string) (string, error) {
	c := path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if p == "" || c == "." || c == ".." || strings.HasPrefix(c, "../") || path.IsAbs(c) || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("path %q is absolute or leaves its folder", p)
	}
	return c, nil
}

// Files lists what a brand package ships: brandkit.json, the generated
// outputs, the README brand sheet and its images, and its install list
// (folders whole), sorted.
func Files(pkgDir string) ([]string, *brand.Config, error) {
	b, err := brand.Load(pkgDir)
	if err != nil {
		return nil, nil, fmt.Errorf("the brand package doesn't load: %w", err)
	}
	c := b.Config
	set := map[string]bool{brand.ConfigFile: true}
	var files, folders []string
	for _, f := range []string{c.TokensCSS.File, c.BrandCSS.File, c.DaisyUI.ThemesCSS, c.DaisyUI.PluginCSS, c.Lint} {
		files = append(files, path.Join(c.Out, f))
	}
	files = append(files, c.Readme.File)
	folders = append(folders, c.Readme.Assets)
	folders = append(folders, c.Install...)
	for _, f := range files {
		rel, err := SafeRel(f)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", brand.ConfigFile, err)
		}
		set[rel] = true
	}
	for _, inc := range folders {
		rel, err := SafeRel(inc)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: install entry: %w", brand.ConfigFile, err)
		}
		root := filepath.Join(pkgDir, filepath.FromSlash(rel))
		err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				rel, _ := filepath.Rel(pkgDir, p)
				set[filepath.ToSlash(rel)] = true
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	var out []string
	for f := range set {
		if st, err := os.Lstat(filepath.Join(pkgDir, filepath.FromSlash(f))); err != nil || !st.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("%s is missing from the package (was it built and committed?)", f)
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out, &c, nil
}

// checkEntry rejects a lock entry whose paths are absolute or leave the
// project, or whose repo isn't owner/name.
func checkEntry(e *Entry) error {
	if _, _, err := ParseRef(e.Repo); err != nil {
		return fmt.Errorf("%s: repo %w", LockFile, err)
	}
	if _, err := SafeRel(e.Dir); err != nil {
		return fmt.Errorf("%s: %s: dir: %w", LockFile, e.Repo, err)
	}
	if e.Lint != "" {
		if _, err := SafeRel(e.Lint); err != nil {
			return fmt.Errorf("%s: %s: lint: %w", LockFile, e.Repo, err)
		}
	}
	for f := range e.Files {
		if _, err := SafeRel(f); err != nil {
			return fmt.Errorf("%s: %s: files: %w", LockFile, e.Repo, err)
		}
	}
	return nil
}

// ReadLock reads brandkit.lock from a project (an empty lock if none).
func ReadLock(project string) (*Lock, error) {
	data, err := os.ReadFile(filepath.Join(project, LockFile))
	if errors.Is(err, os.ErrNotExist) {
		return &Lock{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", LockFile, err)
	}
	for i := range l.Brands {
		if err := checkEntry(&l.Brands[i]); err != nil {
			return nil, err
		}
	}
	return &l, nil
}

// writeLock replaces brandkit.lock atomically: a temporary file in the
// project, renamed over the old lock.
func writeLock(project string, l *Lock) error {
	sort.Slice(l.Brands, func(i, j int) bool { return l.Brands[i].Repo < l.Brands[j].Repo })
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(project, ".brandkit.lock-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o644)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(project, LockFile))
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// noSymlinks checks every existing component of rel below project, rel
// itself included: each must be a real folder, not a symlink, so nothing
// brandkit removes or writes there can land outside the project. A missing
// component ends the check; brandkit creates the rest as real folders.
func noSymlinks(project, rel string) error {
	p := project
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		p = filepath.Join(p, part)
		st, err := os.Lstat(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		at := strings.Join(parts[:i+1], "/")
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; brandkit won't install through it, because it could write outside the project. Replace it with a real folder or pick another --dir", at)
		}
		if !st.IsDir() {
			return fmt.Errorf("%s isn't a folder", at)
		}
	}
	return nil
}

// within reports whether a is b or a folder inside it (slash paths).
func within(a, b string) bool { return a == b || strings.HasPrefix(a, b+"/") }

// Install vendors repo at tag into project/dir/<repo name> and records it.
// verify, when set, is the entry the lock already holds: the files must
// hash the same.
//
// The new tree is staged next to the destination and checked first; only
// then is it renamed into place and the lock replaced. On any error the
// installed tree and the lock are left as they were.
func (c *Client) Install(project, repo, tag, dir string, verify *Entry) (*Entry, error) {
	if dir == "" {
		dir = DefaultDir
	}
	if _, _, err := ParseRef(repo); err != nil {
		return nil, err
	}
	dir, err := SafeRel(filepath.ToSlash(dir))
	if err != nil {
		return nil, fmt.Errorf("--dir: %w", err)
	}
	name := repo[strings.Index(repo, "/")+1:]
	dest := path.Join(dir, name)
	l, err := ReadLock(project)
	if err != nil {
		return nil, err
	}
	for _, x := range l.Brands {
		if x.Repo != repo && (within(dest, x.Dir) || within(x.Dir, dest)) {
			return nil, fmt.Errorf("%s would go in %s, which %s already holds (see %s); install it into another folder, for example --dir %s",
				repo, dest, x.Repo, LockFile, path.Join(dir, repo[:strings.Index(repo, "/")]))
		}
	}
	if err := noSymlinks(project, dest); err != nil {
		return nil, err
	}
	commit, err := c.Commit(repo, tag)
	if err != nil {
		return nil, err
	}
	if verify != nil && verify.Commit != commit {
		return nil, fmt.Errorf("%s@%s now points at %s, but %s pins %s; run brandkit update to move", repo, tag, short(commit), LockFile, short(verify.Commit))
	}
	src, err := c.fetch(repo, commit)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(src)
	files, cfg, err := Files(src)
	if err != nil {
		return nil, fmt.Errorf("%s@%s: %w", repo, tag, err)
	}
	e := &Entry{Repo: repo, Tag: tag, Commit: commit, Dir: dest, Lint: path.Join(dest, cfg.Out, cfg.Lint), Files: map[string]string{}}

	destAbs := filepath.Join(project, filepath.FromSlash(dest))
	parent := filepath.Dir(destAbs)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	// MkdirAll creates real folders, but it follows any symlink that
	// appeared since the check, so check again.
	if err := noSymlinks(project, dest); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(parent, ".brandkit-stage-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		e.Files[f] = hex.EncodeToString(sum[:])
		p := filepath.Join(stage, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, err
		}
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return nil, err
	}
	if verify != nil {
		for f, h := range verify.Files {
			if e.Files[f] != h {
				return nil, fmt.Errorf("%s: %s differs from the hash in %s; the installed files were left as they were", repo, f, LockFile)
			}
		}
	}

	// Swap: move the old tree aside, the staged one in, then the lock.
	old := ""
	if _, err := os.Lstat(destAbs); err == nil {
		old = stage + ".old"
		if err := os.Rename(destAbs, old); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	restore := func() {
		os.RemoveAll(destAbs)
		if old != "" {
			os.Rename(old, destAbs)
		}
	}
	if err := os.Rename(stage, destAbs); err != nil {
		restore()
		return nil, err
	}
	l.Version = 1
	l.Brands = slices.DeleteFunc(l.Brands, func(x Entry) bool { return x.Repo == repo })
	l.Brands = append(l.Brands, *e)
	if err := writeLock(project, l); err != nil {
		restore()
		return nil, err
	}
	if old != "" {
		os.RemoveAll(old)
	}
	return e, nil
}

// Verify checks the vendored files against the lock without the network
// (brandkit install --check). It returns the brands checked, and an error
// listing every file that is missing or was edited.
func Verify(project string) (int, error) {
	l, err := ReadLock(project)
	if err != nil {
		return 0, err
	}
	var problems []string
	for _, e := range l.Brands {
		if err := noSymlinks(project, e.Dir); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", e.Repo, err))
			continue
		}
		for f, h := range e.Files {
			data, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(e.Dir), filepath.FromSlash(f)))
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %s is missing", e.Repo, f))
				continue
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != h {
				problems = append(problems, fmt.Sprintf("%s: %s was edited (it differs from %s)", e.Repo, f, LockFile))
			}
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return len(l.Brands), &VerifyError{Problems: problems}
	}
	return len(l.Brands), nil
}

// VerifyError lists the vendored files that don't match the lock.
type VerifyError struct{ Problems []string }

func (e *VerifyError) Error() string { return strings.Join(e.Problems, "\n") }

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
