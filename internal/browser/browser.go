// Package browser finds and starts the headless Chrome brandkit drives, or
// attaches to one another program (an e2e script) already drives.
package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// ErrNoChrome is returned when no browser can be found.
var ErrNoChrome = errors.New("no Chrome found: run `mise install` in a project that pins http:chrome-headless-shell (as brandkit's mise.toml does), or pass --chrome / set BRANDKIT_CHROME")

// Find returns the browser to launch: explicit, then $BRANDKIT_CHROME, then
// mise's chrome-headless-shell, then a Chrome or Chromium on PATH.
func Find(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if p := os.Getenv("BRANDKIT_CHROME"); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("chrome-headless-shell"); err == nil {
		return p, nil
	}
	if m, err := exec.LookPath("mise"); err == nil {
		out, err := exec.Command(m, "which", "chrome-headless-shell").Output()
		if p := strings.TrimSpace(string(out)); err == nil && p != "" {
			return p, nil
		}
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	if runtime.GOOS == "darwin" {
		p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", ErrNoChrome
}

// Launch starts a headless browser with settings that keep screenshots the
// same from run to run. Cancel the returned context to stop it.
func Launch(ctx context.Context, chrome string) (context.Context, context.CancelFunc, error) {
	path, err := Find(chrome)
	if err != nil {
		return nil, nil, err
	}
	opts := append(slices.Clone(chromedp.DefaultExecAllocatorOptions[:]),
		chromedp.ExecPath(path),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("font-render-hinting", "none"),
		chromedp.Flag("disable-lcd-text", true),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("disable-font-subpixel-positioning", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-features", "Translate,MediaRouter,OptimizationHints,PaintHolding"),
		chromedp.Flag("lang", "en-US"),
	)
	if (runtime.GOOS == "linux" && os.Geteuid() == 0) || os.Getenv("BRANDKIT_NO_SANDBOX") == "1" {
		opts = append(opts, chromedp.NoSandbox)
	}
	actx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	cctx, cancelCtx := chromedp.NewContext(actx, chromedp.WithErrorf(func(string, ...any) {}))
	if err := chromedp.Do(cctx); err != nil {
		cancelCtx()
		cancelAlloc()
		return nil, nil, fmt.Errorf("starting %s: %w", path, err)
	}
	return cctx, func() { cancelCtx(); cancelAlloc() }, nil
}

// Attached is a tab brandkit opened in a browser it attached to.
type Attached struct {
	Ctx context.Context
	// URL is the URL of the page the tab's browser context was chosen by.
	URL       string
	ContextID cdp.BrowserContextID
	cancel    []context.CancelFunc
}

// Close closes brandkit's tab and detaches, leaving the browser and the
// other program's pages as they were.
func (a *Attached) Close() {
	for i := len(a.cancel) - 1; i >= 0; i-- {
		a.cancel[i]()
	}
}

// Attach connects to a running browser over its DevTools websocket (or
// http://host:port), picks the page whose URL contains match (the first
// page when match is empty), and opens a new tab in that page's browser
// context, so it shares the cookies and storage of the signed-in session.
func Attach(ctx context.Context, ws, match string) (*Attached, error) {
	actx, cancelAlloc := remote.NewAllocator(ctx, ws)
	bctx, cancelB := chromedp.NewContext(actx, chromedp.WithErrorf(func(string, ...any) {}))
	a := &Attached{cancel: []context.CancelFunc{cancelAlloc, cancelB}}
	targets, err := chromedp.Targets(bctx)
	if err != nil {
		a.Close()
		return nil, fmt.Errorf("attaching to %s: %w", ws, err)
	}
	var pages []string
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		pages = append(pages, t.URL)
		if match == "" || strings.Contains(t.URL, match) {
			a.URL, a.ContextID = t.URL, t.BrowserContextID
			break
		}
	}
	if a.URL == "" {
		a.Close()
		return nil, fmt.Errorf("no page in that browser matches %q (pages: %s)", match, strings.Join(pages, ", "))
	}
	var opts []chromedp.ContextOption
	if a.ContextID != "" {
		opts = append(opts, chromedp.WithExistingBrowserContext(a.ContextID))
	}
	tctx, cancelT := chromedp.NewContext(bctx, opts...)
	a.cancel = append(a.cancel, cancelT)
	if err := chromedp.Do(tctx); err != nil {
		a.Close()
		return nil, fmt.Errorf("opening a tab: %w", err)
	}
	a.Ctx = tctx
	return a, nil
}

// Settle waits for fonts and two animation frames, so layout and webfonts
// are done before a screenshot.
func Settle(ctx context.Context) error {
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return chromedp.Do(tctx, chromedp.Evaluate[chromedp.Void](
		`document.fonts.ready.then(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))))`,
		chromedp.EvalAwaitPromise))
}
