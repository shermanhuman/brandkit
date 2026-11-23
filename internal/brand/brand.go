// Package brand loads a brand package: its brandkit.json, its DTCG
// resolver, and the tokens resolved once per theme.
package brand

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shermanhuman/brandkit/internal/dtcg"
)

// ConfigFile is the brand package's manifest, at the package root.
const ConfigFile = "brandkit.json"

// Extension keys brandkit reads from token documents.
const (
	ExtBrand   = "com.mapleset.brand"   // brand rules (root $extensions)
	ExtDaisyUI = "com.mapleset.daisyui" // DaisyUI theme mapping (root $extensions)
	ExtCSS     = "com.mapleset.css"     // per-token CSS options, such as {"name": "bg"}
)

// Config is brandkit.json.
type Config struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Resolver    string `json:"resolver"`
	// Modifier is the resolver modifier whose contexts are the themes
	// (default "theme").
	Modifier string                 `json:"modifier,omitempty"`
	Themes   map[string]ThemeConfig `json:"themes"`
	// Out is the folder generated files go to, relative to the package.
	Out       string          `json:"out,omitempty"`
	TokensCSS TokensCSSConfig `json:"tokensCss"`
	BrandCSS  BrandCSSConfig  `json:"brandCss"`
	DaisyUI   DaisyUIConfig   `json:"daisyui"`
	Lint      string          `json:"lint,omitempty"`
	Readme    ReadmeConfig    `json:"readme"`
	Fonts     []FontConfig    `json:"fonts,omitempty"`
	// Install lists the files (relative to the package) a project's
	// `brandkit install` vendors, besides the generated ones. Folders copy whole.
	Install []string `json:"install,omitempty"`
	// Repo is the brand's GitHub owner/name, used in the README's install section.
	Repo string `json:"repo,omitempty"`
}

// ThemeConfig describes one context of the theme modifier.
type ThemeConfig struct {
	Label       string `json:"label,omitempty"`    // "Night"
	DaisyUI     string `json:"daisyui"`            // the DaisyUI theme name
	ColorScheme string `json:"colorScheme"`        // dark | light
	Selector    string `json:"selector,omitempty"` // CSS selector for this theme (default [data-theme="<daisyui>"])
	Default     bool   `json:"default,omitempty"`
	PrefersDark bool   `json:"prefersDark,omitempty"`
	// GitHub is the GitHub colour scheme the README shows this theme in: dark or light.
	GitHub string `json:"github,omitempty"`
}

// TokensCSSConfig controls the legacy-compatible tokens.css.
type TokensCSSConfig struct {
	File string `json:"file,omitempty"` // default ui/tokens.css
	// Include lists token path prefixes (dotted) or $type names written to
	// this file; default ["type:color"].
	Include []string `json:"include,omitempty"`
	// Template is a Go text/template (relative to the package) that writes
	// the whole file instead of the default layout; see build.TemplateData.
	Template string `json:"template,omitempty"`
	Header   string `json:"header,omitempty"`
}

// BrandCSSConfig controls brand.css: every token not in tokens.css.
type BrandCSSConfig struct {
	File string `json:"file,omitempty"` // default ui/brand.css
}

// DaisyUIConfig controls the two DaisyUI theme forms.
type DaisyUIConfig struct {
	ThemesCSS   string `json:"themesCss,omitempty"`   // plain [data-theme] form, default ui/daisyui-themes.css
	PluginCSS   string `json:"pluginCss,omitempty"`   // @plugin form, default ui/daisyui-plugin.css
	Plugin      string `json:"plugin,omitempty"`      // default ../vendor/daisyui ("-" to leave the line out)
	ThemePlugin string `json:"themePlugin,omitempty"` // default ../vendor/daisyui-theme
	// KeepPalette leaves Tailwind's default colours in place. By default the
	// @plugin form resets them (--color-*: initial) so bg-blue-500 and the
	// like don't exist.
	KeepPalette bool `json:"keepPalette,omitempty"`
}

// ReadmeConfig controls the generated brand sheet.
type ReadmeConfig struct {
	File   string `json:"file,omitempty"`   // default README.md
	Assets string `json:"assets,omitempty"` // default <out>/sheet
}

// FontConfig is one font family the brand ships.
type FontConfig struct {
	Family string `json:"family"`
	// CSS is a stylesheet with the @font-face rules (relative to the package).
	CSS     string `json:"css"`
	License string `json:"license,omitempty"`
}

// Theme is the tokens resolved for one theme.
type Theme struct {
	Name   string
	Config ThemeConfig
	Tokens *dtcg.Tokens
}

// Brand is a loaded brand package.
type Brand struct {
	Dir      string
	Config   Config
	Resolver *dtcg.Resolver
	Themes   []*Theme // the default theme first, then in resolver order
}

// Load reads a brand package from dir.
func Load(dir string) (*Brand, error) {
	data, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		return nil, err
	}
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	c.defaults()
	if c.Name == "" || c.Resolver == "" {
		return nil, fmt.Errorf("%s: name and resolver are required", ConfigFile)
	}
	r, err := dtcg.LoadResolver(filepath.Join(dir, c.Resolver))
	if err != nil {
		return nil, err
	}
	if err := findTodos(r); err != nil {
		return nil, err
	}
	b := &Brand{Dir: dir, Config: c, Resolver: r}
	var mod *dtcg.Modifier
	for _, m := range r.Modifiers() {
		if m.Name == c.Modifier {
			mod = &m
		}
	}
	if mod == nil {
		return nil, fmt.Errorf("the resolver has no %q modifier for the themes", c.Modifier)
	}
	defaults := 0
	for _, ctx := range mod.Contexts {
		tc, ok := c.Themes[ctx]
		if !ok {
			return nil, fmt.Errorf("%s: themes has no entry for the %s context %q", ConfigFile, c.Modifier, ctx)
		}
		if tc.Default {
			defaults++
		}
		toks, err := r.Resolve(map[string]string{c.Modifier: ctx})
		if err != nil {
			return nil, fmt.Errorf("theme %s: %w", ctx, err)
		}
		th := &Theme{Name: ctx, Config: tc, Tokens: toks}
		if tc.Default {
			b.Themes = append([]*Theme{th}, b.Themes...)
		} else {
			b.Themes = append(b.Themes, th)
		}
	}
	for name := range c.Themes {
		if !slices.Contains(mod.Contexts, name) {
			return nil, fmt.Errorf("%s: theme %q isn't a context of the %s modifier", ConfigFile, name, c.Modifier)
		}
	}
	if defaults != 1 {
		return nil, fmt.Errorf("%s: exactly one theme must be the default (found %d)", ConfigFile, defaults)
	}
	return b, nil
}

// TodoError lists token values left as the placeholder "TODO".
type TodoError struct{ Tokens []string }

func (e *TodoError) Error() string {
	return fmt.Sprintf("%d token values are the placeholder \"TODO\": %s", len(e.Tokens), strings.Join(e.Tokens, ", "))
}

// findTodos fails a package whose token files hold a $value (or part of a
// composite $value) that is the string "TODO", naming each token.
func findTodos(r *dtcg.Resolver) error {
	var found []string
	for _, f := range r.Files() {
		data, err := os.ReadFile(filepath.Join(r.Dir, filepath.FromSlash(f)))
		if err != nil {
			return err
		}
		doc, err := dtcg.Decode(data)
		if err != nil {
			return err
		}
		var walk func(v any, path []string)
		walk = func(v any, path []string) {
			o, ok := v.(*dtcg.Object)
			if !ok {
				return
			}
			if val, ok := o.Get("$value"); ok {
				if hasTodo(val) {
					found = append(found, f+": "+strings.Join(path, "."))
				}
				return
			}
			for _, k := range o.Keys {
				if !strings.HasPrefix(k, "$") {
					walk(o.Values[k], append(slices.Clone(path), k))
				}
			}
		}
		walk(doc, nil)
	}
	if len(found) > 0 {
		return &TodoError{Tokens: found}
	}
	return nil
}

func hasTodo(v any) bool {
	switch x := v.(type) {
	case string:
		return x == "TODO"
	case []any:
		return slices.ContainsFunc(x, hasTodo)
	case *dtcg.Object:
		for _, k := range x.Keys {
			if hasTodo(x.Values[k]) {
				return true
			}
		}
	}
	return false
}

func (c *Config) defaults() {
	if c.Modifier == "" {
		c.Modifier = "theme"
	}
	if c.Out == "" {
		c.Out = "export"
	}
	if c.TokensCSS.File == "" {
		c.TokensCSS.File = "ui/tokens.css"
	}
	if len(c.TokensCSS.Include) == 0 {
		c.TokensCSS.Include = []string{"type:color"}
	}
	if c.BrandCSS.File == "" {
		c.BrandCSS.File = "ui/brand.css"
	}
	if c.DaisyUI.ThemesCSS == "" {
		c.DaisyUI.ThemesCSS = "ui/daisyui-themes.css"
	}
	if c.DaisyUI.PluginCSS == "" {
		c.DaisyUI.PluginCSS = "ui/daisyui-plugin.css"
	}
	if c.DaisyUI.Plugin == "" {
		c.DaisyUI.Plugin = "../vendor/daisyui"
	}
	if c.DaisyUI.ThemePlugin == "" {
		c.DaisyUI.ThemePlugin = "../vendor/daisyui-theme"
	}
	if c.Lint == "" {
		c.Lint = "lint.json"
	}
	if c.Readme.File == "" {
		c.Readme.File = "README.md"
	}
	if c.Readme.Assets == "" {
		c.Readme.Assets = c.Out + "/sheet"
	}
	for k, t := range c.Themes {
		if t.Label == "" {
			t.Label = strings.ToUpper(k[:1]) + k[1:]
		}
		if t.Selector == "" {
			t.Selector = fmt.Sprintf(`[data-theme="%s"]`, t.DaisyUI)
		}
		if t.GitHub == "" {
			t.GitHub = t.ColorScheme
		}
		c.Themes[k] = t
	}
}

// Default returns the default theme.
func (b *Brand) Default() *Theme { return b.Themes[0] }

// Theme returns a theme by name, or nil.
func (b *Brand) Theme(name string) *Theme {
	for _, t := range b.Themes {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// OutPath is a generated file's path inside the package.
func (b *Brand) OutPath(rel string) string {
	return filepath.Join(b.Dir, b.Config.Out, filepath.FromSlash(rel))
}

// Rules returns the brand rules ($extensions["com.mapleset.brand"]) of a
// theme, decoded into r.
func (t *Theme) Rules() (*Rules, error) {
	r := &Rules{}
	ext := t.Tokens.Extensions.Object(ExtBrand)
	if ext == nil {
		return r, nil
	}
	b, err := json.Marshal(ext)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(r); err != nil {
		return nil, fmt.Errorf("$extensions[%q]: %w", ExtBrand, err)
	}
	return r, nil
}

// Rules are the brand rules that brandkit checks and lints.
type Rules struct {
	// Shadows is "none" when the brand bans shadows and glows.
	Shadows string `json:"shadows,omitempty"`
	// Mark is the logo mark; Lockup is the lockup to use on each theme.
	Mark   string            `json:"mark,omitempty"`
	Lockup map[string]string `json:"lockup,omitempty"`
	// ClearSpace is the space kept around the lockup, as a multiple of the
	// mark's height.
	ClearSpace *ClearSpace `json:"clearSpace,omitempty"`
	// Accent names the accent colour and the body-text roles it may never be.
	Accent *AccentRule `json:"accent,omitempty"`
	Icons  *IconRule   `json:"icons,omitempty"`
	// Radius lists the radius tokens, smallest first.
	Radius []string `json:"radius,omitempty"`
	// Spacing lists the spacing tokens, smallest first.
	Spacing []string `json:"spacing,omitempty"`
	// Type lists the typography tokens for the specimen, largest first.
	Type []string `json:"type,omitempty"`
	// Fonts maps a role (sans, mono, display) to a fontFamily token.
	Fonts map[string]string `json:"fonts,omitempty"`
	// Contrast lists the colour pairs that must meet a WCAG 2.x ratio.
	Contrast []ContrastPair `json:"contrast,omitempty"`
	// Roles describes the colour roles for the sheet: token → what it's for.
	Roles []Role   `json:"roles,omitempty"`
	Do    []string `json:"do,omitempty"`
	Dont  []string `json:"dont,omitempty"`
	// OpacityMin is the lowest opacity modifier (percent) R8 lets through
	// on a text colour role without a warning (default 70).
	OpacityMin int `json:"opacityMin,omitempty"`
}

// ClearSpace is the lockup's clear-space rule.
type ClearSpace struct {
	Ratio float64 `json:"ratio"`
	Of    string  `json:"of"` // what the ratio multiplies, such as "mark height"
	Note  string  `json:"note,omitempty"`
}

// AccentRule keeps the accent colour off body text.
type AccentRule struct {
	Token    string   `json:"token"`
	BodyText []string `json:"bodyText"`
	Note     string   `json:"note,omitempty"`
}

// IconRule names the icon set.
type IconRule struct {
	Set         string   `json:"set"`
	StrokeWidth float64  `json:"strokeWidth,omitempty"`
	Usage       string   `json:"usage,omitempty"`  // how a template draws one
	Prefix      string   `json:"prefix,omitempty"` // name prefix, such as "lucide-"
	Examples    []string `json:"examples,omitempty"`
}

// ContrastPair is a foreground/background pair and its minimum ratio.
type ContrastPair struct {
	FG  string  `json:"fg"`
	BG  string  `json:"bg"`
	Min float64 `json:"min"`
	Use string  `json:"use,omitempty"`
}

// Role is one row of the colour table.
type Role struct {
	Token string `json:"token"`
	Use   string `json:"use"`
}
