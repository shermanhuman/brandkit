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

// untar extracts regular files, dropping the archive's top folder.
func untar(r io.Reader, dir string) error {
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
		if _, err := io.Copy(f, io.LimitReader(tr, 200<<20)); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
}

// Files lists what a brand package ships: brandkit.json, the generated
// outputs, and its install list (folders whole), sorted.
func Files(pkgDir string) ([]string, *brand.Config, error) {
	b, err := brand.Load(pkgDir)
	if err != nil {
		return nil, nil, fmt.Errorf("the brand package doesn't load: %w", err)
	}
	c := b.Config
	set := map[string]bool{brand.ConfigFile: true}
	for _, f := range []string{c.TokensCSS.File, c.BrandCSS.File, c.DaisyUI.ThemesCSS, c.DaisyUI.PluginCSS, c.Lint} {
		set[path.Join(c.Out, f)] = true
	}
	for _, inc := range c.Install {
		inc = path.Clean(inc)
		if strings.HasPrefix(inc, "..") || path.IsAbs(inc) {
			return nil, nil, fmt.Errorf("install entry %q leaves the package", inc)
		}
		root := filepath.Join(pkgDir, filepath.FromSlash(inc))
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
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
		if _, err := os.Stat(filepath.Join(pkgDir, filepath.FromSlash(f))); err != nil {
			return nil, nil, fmt.Errorf("%s is missing from the package (was it built and committed?)", f)
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out, &c, nil
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
	return &l, nil
}

func writeLock(project string, l *Lock) error {
	sort.Slice(l.Brands, func(i, j int) bool { return l.Brands[i].Repo < l.Brands[j].Repo })
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(project, LockFile), append(data, '\n'), 0o644)
}

// Install vendors repo at tag into project/dir/<repo name> and records it.
// verify, when set, is the entry the lock already holds: the files must
// hash the same.
func (c *Client) Install(project, repo, tag, dir string, verify *Entry) (*Entry, error) {
	if dir == "" {
		dir = DefaultDir
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
	name := repo[strings.Index(repo, "/")+1:]
	dest := path.Join(filepath.ToSlash(dir), name)
	e := &Entry{Repo: repo, Tag: tag, Commit: commit, Dir: dest, Lint: path.Join(dest, cfg.Out, cfg.Lint), Files: map[string]string{}}
	destAbs := filepath.Join(project, filepath.FromSlash(dest))
	if err := os.RemoveAll(destAbs); err != nil {
		return nil, err
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		e.Files[f] = hex.EncodeToString(sum[:])
		p := filepath.Join(destAbs, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, err
		}
	}
	if verify != nil {
		for f, h := range verify.Files {
			if e.Files[f] != h {
				return nil, fmt.Errorf("%s: %s differs from the hash in %s", repo, f, LockFile)
			}
		}
	}
	l, err := ReadLock(project)
	if err != nil {
		return nil, err
	}
	l.Version = 1
	l.Brands = slices.DeleteFunc(l.Brands, func(x Entry) bool { return x.Repo == repo })
	l.Brands = append(l.Brands, *e)
	return e, writeLock(project, l)
}

// Verify checks the vendored files against the lock without the network.
func Verify(project string) error {
	l, err := ReadLock(project)
	if err != nil {
		return err
	}
	var problems []string
	for _, e := range l.Brands {
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
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
