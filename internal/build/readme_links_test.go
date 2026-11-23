package build

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var linkRe = regexp.MustCompile(`\]\(([^)#\s]+)\)|src="([^"]+)"|srcset="([^"]+)"`)

// TestReadmeLinksInSubfolder: with the README in a subfolder, every
// relative link and image in the sheet resolves from that folder.
func TestReadmeLinksInSubfolder(t *testing.T) {
	b := load(t, "base")
	b.Config.Readme.File = "export/README.md"
	files, err := Sheet(b)
	if err != nil {
		t.Fatal(err)
	}
	made := map[string]bool{}
	var readme string
	for _, f := range files {
		made[f.Path] = true
		if f.Path == b.Config.Readme.File {
			readme = string(f.Data)
		}
	}
	if readme == "" {
		t.Fatal("no README in the sheet output")
	}
	for _, m := range linkRe.FindAllStringSubmatch(readme, -1) {
		target := m[1] + m[2] + m[3]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue
		}
		p := path.Clean(path.Join("export", target))
		if made[p] {
			continue
		}
		if _, err := os.Stat(filepath.Join(b.Dir, filepath.FromSlash(p))); err != nil {
			t.Errorf("link %q resolves to %s, which doesn't exist", target, p)
		}
	}
}
