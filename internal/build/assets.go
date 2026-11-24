package build

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif" // decoders for asset previews
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/shermanhuman/brandkit/internal/brand"
	"github.com/shermanhuman/brandkit/internal/color"
)

// The Brand assets section shows what a project gets from the package's
// `install` list besides tokens. The rule:
//
//   - every file under an `install` entry, folders walked whole;
//   - except token sources (*.tokens.json, *.resolver.json), Markdown
//     files, dotfiles, the README and its sheet folder, and the files
//     brandkit generates;
//   - grouped by folder, in `install` order; within a folder, files whose
//     names differ only in extension or a -1x/-2x/@2x scale suffix share a
//     row (mapleset-leaf-plum.svg and mapleset-leaf-plum-4x.png).
//
// A row shows a preview when it has an SVG or a PNG, JPEG or GIF. An image
// with transparency is drawn on the theme ground it reads best on (the
// theme it's the lockup for, else the base-100 with the most contrast to
// its colours); an opaque image is shown as it is.

// AssetRule is the Brand assets rule in one line, as the sheet states it.
const AssetRule = "Everything the package's `install` list ships except token files, Markdown, the README and the files brandkit generates, grouped by folder. Files that differ only in format or scale (`-2x`) share a row; transparent images are drawn on the theme ground they're made for."

var scaleSuffixRe = regexp.MustCompile(`(?:[-@]\d+(?:\.\d+)?x)$`)

type assetGroup struct {
	stem  string
	files []string // package paths, sorted
}

type assetDir struct {
	dir    string
	groups []*assetGroup
}

func (s *sheet) skipAsset(p string) bool {
	c := s.b.Config
	base := path.Base(p)
	if strings.HasPrefix(base, ".") || strings.HasSuffix(base, ".tokens.json") || strings.HasSuffix(base, ".resolver.json") ||
		strings.EqualFold(path.Ext(base), ".md") || p == c.Readme.File || p == brand.ConfigFile {
		return true
	}
	if strings.HasPrefix(p, strings.TrimSuffix(c.Readme.Assets, "/")+"/") {
		return true
	}
	out := func(f string) string { return pathJoin(c.Out, f) }
	return slices.Contains([]string{out(c.TokensCSS.File), out(c.BrandCSS.File), out(c.DaisyUI.ThemesCSS), out(c.DaisyUI.PluginCSS), out(c.Lint)}, p)
}

// assetDirs lists the install files the Brand assets section shows.
func (s *sheet) assetDirs() ([]*assetDir, error) {
	var dirs []*assetDir
	byDir := map[string]*assetDir{}
	seen := map[string]bool{}
	for _, entry := range s.b.Config.Install {
		entry = path.Clean(filepath.ToSlash(entry))
		root := filepath.Join(s.b.Dir, filepath.FromSlash(entry))
		err := filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(s.b.Dir, fp)
			if err != nil {
				return err
			}
			p := filepath.ToSlash(rel)
			if d.IsDir() {
				if p != entry && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || seen[p] || s.skipAsset(p) {
				return nil
			}
			seen[p] = true
			dir := path.Dir(p)
			ad := byDir[dir]
			if ad == nil {
				ad = &assetDir{dir: dir}
				byDir[dir] = ad
				dirs = append(dirs, ad)
			}
			stem := strings.TrimSuffix(path.Base(p), path.Ext(p))
			stem = scaleSuffixRe.ReplaceAllString(stem, "")
			for _, g := range ad.groups {
				if g.stem == stem {
					g.files = append(g.files, p)
					return nil
				}
			}
			ad.groups = append(ad.groups, &assetGroup{stem: stem, files: []string{p}})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("install: %w", err)
		}
	}
	for _, ad := range dirs {
		sort.SliceStable(ad.groups, func(i, j int) bool { return strings.ToLower(ad.groups[i].stem) < strings.ToLower(ad.groups[j].stem) })
		for _, g := range ad.groups {
			sort.SliceStable(g.files, func(i, j int) bool { return strings.ToLower(g.files[i]) < strings.ToLower(g.files[j]) })
		}
	}
	return dirs, nil
}

func (s *sheet) read(p string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.b.Dir, filepath.FromSlash(p)))
}

// preview is how a group is pictured.
type preview struct {
	file   string
	data   []byte
	mime   string
	w, h   float64
	opaque bool
	ink    float64 // mean relative luminance of the drawn pixels or colours
	inked  bool
}

var hexColourRe = regexp.MustCompile(`(?i)(?:fill|stroke|stop-color|color)\s*[:=]\s*"?\s*(#[0-9a-f]{6}|#[0-9a-f]{3})\b`)

func (s *sheet) preview(g *assetGroup) (*preview, error) {
	var best *preview
	for _, f := range g.files {
		ext := strings.ToLower(path.Ext(f))
		switch ext {
		case ".svg":
			data, err := s.read(f)
			if err != nil {
				return nil, err
			}
			w, h, ok := svgSize(data)
			if !ok {
				continue
			}
			pv := &preview{file: f, data: data, mime: "image/svg+xml", w: w, h: h}
			sum, n := 0.0, 0
			for _, m := range hexColourRe.FindAllSubmatch(data, -1) {
				c, err := color.ParseHex(string(m[1]))
				if err == nil {
					sum += c.Luminance()
					n++
				}
			}
			if n > 0 {
				pv.ink, pv.inked = sum/float64(n), true
			}
			return pv, nil // an SVG is the best preview
		case ".png", ".jpg", ".jpeg", ".gif":
			data, err := s.read(f)
			if err != nil {
				return nil, err
			}
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				continue
			}
			b := img.Bounds()
			if best != nil && best.w*best.h >= float64(b.Dx()*b.Dy()) {
				continue
			}
			mime := "image/png"
			if ext == ".gif" {
				mime = "image/gif"
			} else if ext != ".png" {
				mime = "image/jpeg"
			}
			pv := &preview{file: f, data: data, mime: mime, w: float64(b.Dx()), h: float64(b.Dy()), opaque: true}
			sum, wsum := 0.0, 0.0
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					r, g, bl, a := img.At(x, y).RGBA()
					if a < 0xffff {
						pv.opaque = false
					}
					if a == 0 {
						continue
					}
					af := float64(a) / 0xffff
					// un-premultiply
					c := color.RGB{R: float64(r) / float64(a), G: float64(g) / float64(a), B: float64(bl) / float64(a), A: 1}
					sum += c.Luminance() * af
					wsum += af
				}
			}
			if wsum > 0 {
				pv.ink, pv.inked = sum/wsum, true
			}
			best = pv
		}
	}
	return best, nil
}

// groundFor picks the theme an image with transparency is shown on.
func (s *sheet) groundFor(g *assetGroup, pv *preview) *brand.Theme {
	for _, th := range s.b.Themes {
		if slices.Contains(g.files, s.rules.Lockup[th.Name]) {
			return th
		}
	}
	best, bestRatio := s.b.Default(), 0.0
	if !pv.inked {
		return best
	}
	for _, th := range s.b.Themes {
		bg, err := color.ParseHex(s.roleHex(th, "base-100", "#ffffff"))
		if err != nil {
			continue
		}
		hi, lo := max(pv.ink, bg.Luminance()), min(pv.ink, bg.Luminance())
		if r := (hi + 0.05) / (lo + 0.05); r > bestRatio+1e-9 {
			best, bestRatio = th, r
		}
	}
	return best
}

const (
	thumbW, thumbH = 360.0, 96.0
	thumbPad       = 12.0
)

// previewCell writes a group's preview: the image itself when it's opaque,
// otherwise a thumbnail SVG in the sheet folder with the image on its ground.
func (s *sheet) previewCell(g *assetGroup, pv *preview) (string, *brand.Theme) {
	sc := min(thumbW/pv.w, thumbH/pv.h)
	if pv.opaque {
		sc = min(sc, 1)
		return fmt.Sprintf(`<img src="%s" width="%s" alt="">`, s.rel(pv.file), brand.Num(float64(int(pv.w*sc+0.5)))), nil
	}
	sc = min(sc, 8)
	th := s.groundFor(g, pv)
	w, h := pv.w*sc, pv.h*sc
	W, H := w+2*thumbPad, h+2*thumbPad
	render := ""
	if pv.mime != "image/svg+xml" {
		render = ` style="image-rendering: pixelated"`
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s">`, brand.Num(W), brand.Num(H), brand.Num(W), brand.Num(H))
	fmt.Fprintf(&b, `<rect width="%s" height="%s" rx="6" fill="%s"/>`, brand.Num(W), brand.Num(H), s.roleHex(th, "base-100", "#ffffff"))
	fmt.Fprintf(&b, `<image x="%s" y="%s" width="%s" height="%s"%s href="%s"/></svg>`+"\n",
		brand.Num(thumbPad), brand.Num(thumbPad), brand.Num(w), brand.Num(h), render, dataURI(pv.mime, pv.data))
	slug := strings.ReplaceAll(strings.TrimSuffix(pv.file, path.Ext(pv.file)), "/", "-")
	p := s.asset("asset-"+slug+".svg", b.Bytes())
	return fmt.Sprintf(`<img src="%s" alt="">`, s.rel(p)), th
}

var (
	appIconRe = regexp.MustCompile(`^(?:android-chrome|icon)-(\d+)(?:x\d+)?$`)
	licenceRe = regexp.MustCompile(`(?i)^(?:ofl|licen[cs]e|copying)`)
)

// assetUse is a group's one-line use.
func (s *sheet) assetUse(g *assetGroup, pv *preview, ground *brand.Theme) string {
	has := func(ext string) bool {
		for _, f := range g.files {
			if strings.EqualFold(path.Ext(f), ext) {
				return true
			}
		}
		return false
	}
	size := ""
	if pv != nil && pv.mime != "image/svg+xml" {
		size = fmt.Sprintf(", %d×%d", int(pv.w), int(pv.h))
	}
	on := ""
	if ground != nil {
		on = fmt.Sprintf(", for %s grounds (shown on %s)", ground.Config.ColorScheme, ground.Config.Label)
	}
	for _, th := range s.b.Themes {
		if slices.Contains(g.files, s.rules.Lockup[th.Name]) {
			return fmt.Sprintf("the %s lockup, as under Logo and clear space%s", th.Config.Label, on)
		}
	}
	if slices.Contains(g.files, s.rules.Mark) {
		return "the mark on its own" + on
	}
	name := path.Base(g.files[0])
	dir := strings.ToLower(path.Base(path.Dir(g.files[0])))
	switch {
	case g.stem == "favicon":
		var parts []string
		if has(".svg") {
			parts = append(parts, "SVG for current browsers")
		}
		if has(".ico") {
			parts = append(parts, "ICO for older browsers and `/favicon.ico` requests")
		}
		if has(".png") {
			parts = append(parts, "PNG")
		}
		return "the favicon: " + strings.Join(parts, "; ")
	case g.stem == "apple-touch-icon":
		return "home-screen icon for iPhone and iPad" + size
	case strings.HasPrefix(g.stem, "icon-maskable") || strings.HasPrefix(g.stem, "maskable"):
		return "maskable web app icon (Android crops it to its own shape)" + size
	case appIconRe.MatchString(g.stem):
		return "web app icon, listed in the manifest" + size
	case name == "site.webmanifest" || name == "manifest.json" || name == "manifest.webmanifest":
		return "web app manifest: name, icons and theme colour"
	case name == "head.html":
		return "the `<head>` tags that load the favicon set (shown below)"
	}
	for _, f := range s.b.Config.Fonts {
		if slices.Contains(g.files, f.CSS) {
			return "`@font-face` rules for " + s.familiesFor(func(fc brand.FontConfig) bool { return fc.CSS == f.CSS }) + ": link this stylesheet"
		}
		if slices.Contains(g.files, f.License) {
			return "font licence for " + s.familiesFor(func(fc brand.FontConfig) bool { return fc.License == f.License })
		}
	}
	switch ext := strings.ToLower(path.Ext(name)); ext {
	case ".woff2", ".woff", ".ttf", ".otf":
		use := "font file (" + strings.ToUpper(ext[1:]) + ")"
		if css := s.fontCSSLoading(g.files[0]); css != "" {
			fam := s.familiesFor(func(fc brand.FontConfig) bool {
				return fc.CSS == css && strings.Contains(letters(name), letters(fc.Family))
			})
			if fam == "" {
				fam = s.familiesFor(func(fc brand.FontConfig) bool { return fc.CSS == css })
			}
			use = fam + " " + use + ", loaded by `" + path.Base(css) + "`"
		}
		return use
	case ".svg", ".png", ".webp", ".jpg", ".jpeg", ".gif", ".avif":
		noun := "image"
		switch {
		case strings.Contains(strings.ToLower(g.stem), "preview"):
			noun = "preview image"
		case strings.Contains(dir, "lockup") || strings.Contains(dir, "logo"):
			noun = "lockup"
		case strings.Contains(dir, "hero") || strings.Contains(dir, "banner"):
			noun = "hero image"
		case strings.Contains(dir, "social"):
			noun = "social card"
		case strings.Contains(dir, "avatar"):
			noun = "avatar"
		case strings.Contains(dir, "icon") && !strings.Contains(dir, "favicon"):
			noun = "icon"
		}
		return noun + on + size
	case ".css":
		return "stylesheet"
	}
	if licenceRe.MatchString(name) {
		return "licence"
	}
	return "file"
}

// letters is s lower-cased with only its letters and digits.
func letters(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *sheet) familiesFor(keep func(brand.FontConfig) bool) string {
	var fams []string
	for _, f := range s.b.Config.Fonts {
		if keep(f) && !slices.Contains(fams, f.Family) {
			fams = append(fams, f.Family)
		}
	}
	return strings.Join(fams, " and ")
}

// fontCSSLoading finds the brand font stylesheet that loads a font file.
func (s *sheet) fontCSSLoading(file string) string {
	for _, f := range s.b.Config.Fonts {
		data, err := s.read(f.CSS)
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte(path.Base(file))) {
			return f.CSS
		}
	}
	return ""
}

func (s *sheet) assets() error {
	s.p("## Brand assets\n\n")
	dirs, err := s.assetDirs()
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		s.p("The package's `install` list ships no assets besides its tokens.\n\n")
		return nil
	}
	s.p("%s\n\n", AssetRule)
	for _, ad := range dirs {
		s.p("### `%s`\n\n| Preview | Files | Use |\n|---|---|---|\n", ad.dir)
		var head string
		for _, g := range ad.groups {
			pv, err := s.preview(g)
			if err != nil {
				return err
			}
			cell := ""
			var ground *brand.Theme
			if pv != nil {
				cell, ground = s.previewCell(g, pv)
			}
			var links []string
			for _, f := range g.files {
				links = append(links, fmt.Sprintf("[`%s`](%s)", path.Base(f), s.rel(f)))
				if path.Base(f) == "head.html" {
					head = f
				}
			}
			s.p("| %s | %s | %s |\n", cell, strings.Join(links, "<br>"), strings.ReplaceAll(s.assetUse(g, pv, ground), "|", `\|`))
		}
		s.p("\n")
		if head != "" {
			if data, err := s.read(head); err == nil && len(data) <= 2048 && !bytes.Contains(data, []byte("```")) {
				s.p("`%s`, to paste into the page's `<head>` (serve the files from the site root):\n\n```html\n%s\n```\n\n", path.Base(head), strings.TrimRight(string(data), "\n"))
			}
		}
	}
	return nil
}
