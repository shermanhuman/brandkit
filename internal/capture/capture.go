// Package capture renders a page at several widths and reports what a
// reviewer would otherwise check by eye: axe-core violations, the computed
// contrast of every text element, and horizontal overflow, with a
// screenshot per width. The report is the same from run to run for the
// same page and browser.
package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/shermanhuman/brandkit/internal/browser"
	"github.com/shermanhuman/brandkit/internal/color"
)

//go:embed axe/axe.min.js
var axeSource string

// AxeSHA256 is the checksum of the embedded, unmodified axe.min.js 4.14.1.
const AxeSHA256 = "29348999450cdf2849a8e70b7912fc2da85a849da39c0d0d5394f51d4a9ed95c"

// DefaultWidths are the viewport widths captured unless told otherwise.
var DefaultWidths = []int{375, 768, 1280, 1920}

// Options control a capture.
type Options struct {
	URL    string // the page; with CDP, defaults to the matched page's URL
	CDP    string // DevTools websocket (or http://host:port) of a running browser
	Match  string // with CDP: pick the page whose URL contains this
	Out    string // folder for the report and PNGs
	Widths []int
	Height int // viewport height (default 900)
	Chrome string
	// MaxHeight caps a screenshot's height in CSS pixels (default 6000).
	MaxHeight int
	// NoFreeze leaves animations and transitions running.
	NoFreeze bool
	// Timeout for the whole capture (default 2 minutes).
	Timeout time.Duration
}

// Report is report.json.
type Report struct {
	URL     string   `json:"url"`
	Axe     string   `json:"axe"`
	Browser string   `json:"browser"`
	Widths  []Width  `json:"widths"`
	Summary Summary  `json:"summary"`
	Notes   []string `json:"notes,omitempty"`
}

// Summary counts the independent failures.
type Summary struct {
	AxeSeriousCritical int   `json:"axeSeriousCritical"`
	AxeOther           int   `json:"axeOther"`
	ContrastFailures   int   `json:"contrastFailures"`
	OverflowWidths     []int `json:"overflowWidths"`
}

// Failed reports whether anything counts as a failure.
func (s Summary) Failed() bool {
	return s.AxeSeriousCritical > 0 || s.ContrastFailures > 0 || len(s.OverflowWidths) > 0
}

// Width is the result at one viewport width.
type Width struct {
	Width      int           `json:"width"`
	Screenshot string        `json:"screenshot"`
	SHA256     string        `json:"sha256"`
	Height     int           `json:"pageHeight"`
	Overflow   Overflow      `json:"overflow"`
	Axe        []Violation   `json:"axe"`
	Contrast   ContrastCheck `json:"contrast"`
}

// Overflow is horizontal overflow at a width.
type Overflow struct {
	ScrollWidth int        `json:"scrollWidth"`
	Overflows   bool       `json:"overflows"`
	Offenders   []Offender `json:"offenders"`
}

// Offender is an element reaching past the viewport's right edge.
type Offender struct {
	Selector string `json:"selector"`
	Right    int    `json:"right"`
}

// Violation is one axe-core rule that failed.
type Violation struct {
	ID      string   `json:"id"`
	Impact  string   `json:"impact"`
	Help    string   `json:"help"`
	Targets []string `json:"targets"`
}

// ContrastCheck is the computed contrast of text at a width.
type ContrastCheck struct {
	Checked  int               `json:"checked"`
	Skipped  int               `json:"skipped"` // text over images or gradients
	Failures []ContrastFailure `json:"failures"`
}

// ContrastFailure is text below its WCAG 2.x minimum.
type ContrastFailure struct {
	Selector string  `json:"selector"`
	Text     string  `json:"text"`
	FG       string  `json:"fg"`
	BG       string  `json:"bg"`
	Ratio    float64 `json:"ratio"`
	Min      float64 `json:"min"`
}

const freezeCSS = `*, *::before, *::after { animation: none !important; transition: none !important; caret-color: transparent !important; scroll-behavior: auto !important; }`

// Run captures a page and writes report.json and w<width>.png into opts.Out.
func Run(ctx context.Context, opts Options) (*Report, error) {
	if len(opts.Widths) == 0 {
		opts.Widths = DefaultWidths
	}
	if opts.Height == 0 {
		opts.Height = 900
	}
	if opts.MaxHeight == 0 {
		opts.MaxHeight = 6000
	}
	if opts.Timeout == 0 {
		opts.Timeout = 2 * time.Minute
	}
	if opts.Out == "" {
		return nil, fmt.Errorf("capture: an output folder is required")
	}
	ctx, cancelT := context.WithTimeout(ctx, opts.Timeout)
	defer cancelT()
	var tab context.Context
	switch {
	case opts.CDP != "":
		a, err := browser.Attach(ctx, opts.CDP, opts.Match)
		if err != nil {
			return nil, err
		}
		defer a.Close()
		tab = a.Ctx
		if opts.URL == "" {
			opts.URL = a.URL
		}
	case opts.URL != "":
		c, cancel, err := browser.Launch(ctx, opts.Chrome)
		if err != nil {
			return nil, err
		}
		defer cancel()
		tab = c
	default:
		return nil, fmt.Errorf("capture: give --url or --cdp")
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return nil, err
	}
	ua, _ := chromedp.Run(tab, chromedp.Evaluate[string](`navigator.userAgent`))
	rep := &Report{URL: opts.URL, Axe: "axe-core 4.14.1", Browser: browserVersion(ua), Summary: Summary{OverflowWidths: []int{}}}
	if err := chromedp.Do(tab, chromedp.EmulateViewport(int64(opts.Widths[0]), int64(opts.Height)), chromedp.Navigate(opts.URL)); err != nil {
		return nil, fmt.Errorf("capture: loading %s: %w", opts.URL, err)
	}
	if !opts.NoFreeze {
		if err := chromedp.Do(tab, chromedp.Evaluate[chromedp.Void](fmt.Sprintf(
			`(() => { const s = document.createElement('style'); s.textContent = %q; document.documentElement.appendChild(s); })()`, freezeCSS))); err != nil {
			return nil, err
		}
	}
	if err := chromedp.Do(tab, chromedp.Evaluate[chromedp.Void](axeSource+";undefined")); err != nil {
		return nil, fmt.Errorf("capture: loading axe-core: %w", err)
	}
	for _, w := range opts.Widths {
		res, err := one(tab, w, opts)
		if err != nil {
			return nil, fmt.Errorf("capture: width %d: %w", w, err)
		}
		rep.Widths = append(rep.Widths, *res)
		for _, v := range res.Axe {
			if v.Impact == "serious" || v.Impact == "critical" {
				rep.Summary.AxeSeriousCritical++
			} else {
				rep.Summary.AxeOther++
			}
		}
		rep.Summary.ContrastFailures += len(res.Contrast.Failures)
		if res.Overflow.Overflows {
			rep.Summary.OverflowWidths = append(rep.Summary.OverflowWidths, w)
		}
	}
	if err := chromedp.Do(tab, chromedp.ResetViewport()); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(opts.Out, "report.json"), buf.Bytes(), 0o644); err != nil {
		return nil, err
	}
	return rep, nil
}

func browserVersion(ua string) string {
	for _, f := range strings.Fields(ua) {
		if strings.HasPrefix(f, "HeadlessChrome/") || strings.HasPrefix(f, "Chrome/") {
			return f
		}
	}
	return ua
}

type probe struct {
	ScrollWidth int        `json:"scrollWidth"`
	InnerWidth  int        `json:"innerWidth"`
	Height      int        `json:"height"`
	Offenders   []Offender `json:"offenders"`
	Texts       []struct {
		Selector string   `json:"selector"`
		Text     string   `json:"text"`
		Color    string   `json:"color"`
		BGs      []string `json:"bgs"`
		Image    bool     `json:"image"`
		Size     float64  `json:"size"`
		Weight   int      `json:"weight"`
	} `json:"texts"`
}

func one(tab context.Context, w int, opts Options) (*Width, error) {
	if err := chromedp.Do(tab, chromedp.EmulateViewport(int64(w), int64(opts.Height))); err != nil {
		return nil, err
	}
	if err := browser.Settle(tab); err != nil {
		return nil, err
	}
	raw, err := chromedp.Run(tab, chromedp.Evaluate[[]byte](probeJS))
	if err != nil {
		return nil, fmt.Errorf("probing the page: %w", err)
	}
	var pr probe
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, err
	}
	res := &Width{Width: w, Height: pr.Height, Screenshot: fmt.Sprintf("w%d.png", w)}
	res.Overflow = Overflow{ScrollWidth: pr.ScrollWidth, Overflows: pr.ScrollWidth > pr.InnerWidth, Offenders: pr.Offenders}
	if res.Overflow.Offenders == nil {
		res.Overflow.Offenders = []Offender{}
	}
	res.Contrast = contrast(pr)

	axeRaw, err := chromedp.Run(tab, chromedp.Evaluate[[]byte](axeRunJS, chromedp.EvalAwaitPromise))
	if err != nil {
		return nil, fmt.Errorf("running axe: %w", err)
	}
	if err := json.Unmarshal(axeRaw, &res.Axe); err != nil {
		return nil, err
	}
	if res.Axe == nil {
		res.Axe = []Violation{}
	}
	sort.Slice(res.Axe, func(i, j int) bool { return res.Axe[i].ID < res.Axe[j].ID })

	h := min(max(pr.Height, opts.Height), opts.MaxHeight)
	shot, err := chromedp.Call(tab, page.CaptureScreenshot, page.CaptureScreenshotParams{
		Format:                page.CaptureScreenshotFormatPng,
		Clip:                  &page.Viewport{X: 0, Y: 0, Width: float64(w), Height: float64(h), Scale: 1},
		CaptureBeyondViewport: ptr(true),
		FromSurface:           ptr(true),
	})
	if err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}
	if err := os.WriteFile(filepath.Join(opts.Out, res.Screenshot), shot.Data, 0o644); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(shot.Data)
	res.SHA256 = hex.EncodeToString(sum[:])
	// Capturing beyond the viewport can leave the page resized; put the
	// viewport back for the next width.
	if err := chromedp.Do(tab, chromedp.EmulateViewport(int64(w), int64(opts.Height))); err != nil {
		return nil, err
	}
	return res, nil
}

func ptr[T any](v T) *T { return &v }

func contrast(pr probe) ContrastCheck {
	cc := ContrastCheck{Failures: []ContrastFailure{}}
	seen := map[string]bool{}
	for _, t := range pr.Texts {
		if t.Image {
			cc.Skipped++
			continue
		}
		fg, err := color.ParseCSS(t.Color)
		if err != nil {
			cc.Skipped++
			continue
		}
		// Composite the backgrounds from the page down to the element.
		bg := color.RGB{R: 1, G: 1, B: 1, A: 1}
		ok := true
		for i := len(t.BGs) - 1; i >= 0; i-- {
			c, err := color.ParseCSS(t.BGs[i])
			if err != nil {
				ok = false
				break
			}
			bg = c.Over(bg)
		}
		if !ok {
			cc.Skipped++
			continue
		}
		if fg.A == 0 {
			continue
		}
		cc.Checked++
		ratio := color.Contrast(fg.Over(bg), bg)
		need := 4.5
		if t.Size >= 24 || (t.Size >= 18.66 && t.Weight >= 700) {
			need = 3
		}
		if ratio+1e-9 < need {
			f := ContrastFailure{Selector: t.Selector, Text: t.Text, FG: fg.Over(bg).Hex(), BG: bg.Hex(),
				Ratio: float64(int(ratio*100)) / 100, Min: need}
			key := fmt.Sprint(f)
			if !seen[key] {
				seen[key] = true
				cc.Failures = append(cc.Failures, f)
			}
		}
	}
	sort.Slice(cc.Failures, func(i, j int) bool {
		if cc.Failures[i].Selector != cc.Failures[j].Selector {
			return cc.Failures[i].Selector < cc.Failures[j].Selector
		}
		return cc.Failures[i].Text < cc.Failures[j].Text
	})
	return cc
}

const axeRunJS = `axe.run(document, {
  runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'] },
  resultTypes: ['violations'],
}).then(r => r.violations.map(v => ({
  id: v.id, impact: v.impact || '', help: v.help,
  targets: v.nodes.map(n => n.target.map(String).join(' ')).sort(),
})))`

const probeJS = `(() => {
  const sel = (el) => {
    const parts = [];
    for (let e = el; e && e.nodeType === 1 && parts.length < 5; e = e.parentElement) {
      if (e.id) { parts.unshift('#' + e.id); break; }
      const tag = e.tagName.toLowerCase();
      if (tag === 'html' || tag === 'body') { parts.unshift(tag); break; }
      let i = 1;
      for (let s = e.previousElementSibling; s; s = s.previousElementSibling) if (s.tagName === e.tagName) i++;
      parts.unshift(tag + ':nth-of-type(' + i + ')');
    }
    return parts.join(' > ');
  };
  const iw = window.innerWidth;
  const doc = document.documentElement;
  const offenders = [];
  const all = document.body ? document.body.querySelectorAll('*') : [];
  for (const el of all) {
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.right <= iw + 1) continue;
    const p = el.parentElement;
    if (p && p !== document.body && p.getBoundingClientRect().right > iw + 1) continue;
    const cs = getComputedStyle(el);
    if (cs.position === 'fixed' && cs.visibility === 'hidden') continue;
    offenders.push({ selector: sel(el), right: Math.round(r.right + window.scrollX) });
    if (offenders.length >= 20) break;
  }
  const texts = [];
  for (const el of all) {
    if (texts.length >= 3000) break;
    let text = '';
    for (const n of el.childNodes) if (n.nodeType === 3) text += n.textContent;
    text = text.replace(/\s+/g, ' ').trim();
    if (!text) continue;
    const cs = getComputedStyle(el);
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0 || cs.visibility !== 'visible' || cs.display === 'none' || parseFloat(cs.opacity) === 0) continue;
    const bgs = []; let image = false;
    for (let e = el; e; e = e.parentElement) {
      const s = getComputedStyle(e);
      if (s.backgroundImage && s.backgroundImage !== 'none') { image = true; break; }
      bgs.push(s.backgroundColor);
      if (/^rgb\(/.test(s.backgroundColor)) break;
    }
    texts.push({ selector: sel(el), text: text.slice(0, 40), color: cs.color, bgs, image,
      size: parseFloat(cs.fontSize), weight: parseInt(cs.fontWeight, 10) || 400 });
  }
  return { scrollWidth: doc.scrollWidth, innerWidth: iw, height: Math.ceil(Math.max(doc.scrollHeight, document.body ? document.body.scrollHeight : 0)), offenders, texts };
})()`
