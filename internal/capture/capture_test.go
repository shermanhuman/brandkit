package capture

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"

	"github.com/shermanhuman/brandkit/internal/browser"
)

var update = flag.Bool("update", false, "rewrite the golden report")

func TestAxeChecksum(t *testing.T) {
	sum := sha256.Sum256([]byte(axeSource))
	if got := hex.EncodeToString(sum[:]); got != AxeSHA256 {
		t.Fatalf("axe.min.js sha256 %s, want %s: the vendored file must stay unmodified", got, AxeSHA256)
	}
	if !strings.HasPrefix(axeSource, "/*! axe v4.14.1") {
		t.Fatal("not axe-core 4.14.1")
	}
}

// fixtureServer serves the capture page. With cookie set, it answers 401
// until a request carries session=ok (a signed-in screen).
func fixtureServer(t *testing.T, cookie bool, seen *atomic.Int32) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join("..", "..", "testdata")
	mux := http.NewServeMux()
	mux.Handle("/fonts/", http.StripPrefix("/fonts/", http.FileServer(http.Dir(filepath.Join(root, "brands", "base", "fonts")))))
	mux.HandleFunc("/chart.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(root, "capture", "chart.png"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if cookie {
			if c, err := r.Cookie("session"); err != nil || c.Value != "ok" {
				http.Error(w, "signed out", http.StatusUnauthorized)
				return
			}
			seen.Add(1)
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(root, "capture", "page.html"))
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String()
}

func needChrome(t *testing.T) string {
	t.Helper()
	p, err := browser.Find("")
	if err != nil {
		if os.Getenv("BRANDKIT_REQUIRE_TOOLS") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	return p
}

func checkFindings(t *testing.T, rep *Report) {
	t.Helper()
	s := rep.Summary
	if s.AxeSeriousCritical == 0 {
		t.Error("the image without alt text should be a serious/critical axe finding")
	}
	if s.ContrastFailures == 0 {
		t.Error("the faint text should fail contrast")
	}
	if len(s.OverflowWidths) != 1 || s.OverflowWidths[0] != 375 {
		t.Errorf("overflow at %v, want [375]", s.OverflowWidths)
	}
	if len(rep.Widths) != 4 {
		t.Fatalf("%d widths", len(rep.Widths))
	}
	w := rep.Widths[0]
	if len(w.Overflow.Offenders) == 0 || !strings.Contains(w.Overflow.Offenders[0].Selector, "div:nth-of-type(1)") {
		t.Errorf("offenders %v", w.Overflow.Offenders)
	}
	found := false
	for _, f := range w.Contrast.Failures {
		if f.Text == "Last synced a minute ago" && f.FG == "#aeb9c4" && f.BG == "#f8fafd" {
			found = true
		}
	}
	if !found {
		t.Errorf("contrast failures %v", w.Contrast.Failures)
	}
}

// TestGoldenCapture captures the fixture page and compares the whole
// report (PNG hashes included) with the golden one. Linux only.
func TestGoldenCapture(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the golden capture is made on Linux")
	}
	needChrome(t)
	base := fixtureServer(t, false, nil)
	out := t.TempDir()
	rep, err := Run(context.Background(), Options{URL: base + "/", Out: out})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, rep)
	for _, w := range rep.Widths {
		data, err := os.ReadFile(filepath.Join(out, w.Screenshot))
		if err != nil {
			t.Fatal(err)
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != w.SHA256 {
			t.Errorf("%s hash mismatch", w.Screenshot)
		}
	}
	got, _ := os.ReadFile(filepath.Join(out, "report.json"))
	got = []byte(strings.ReplaceAll(string(got), base, "http://fixture"))
	golden := filepath.Join("..", "..", "testdata", "capture", "report.golden.json")
	if *update {
		os.WriteFile(golden, got, 0o644)
		for _, w := range rep.Widths {
			data, _ := os.ReadFile(filepath.Join(out, w.Screenshot))
			os.WriteFile(filepath.Join(filepath.Dir(golden), "golden-"+w.Screenshot), data, 0o644)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("report differs from %s (run go test ./internal/capture -update to refresh, then review):\n%s", golden, got)
	}
	// And again: the same page gives the same report.
	out2 := t.TempDir()
	if _, err := Run(context.Background(), Options{URL: base + "/", Out: out2}); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(out2, "report.json"))
	if strings.ReplaceAll(string(again), base, "http://fixture") != string(got) {
		t.Error("a second run gave a different report")
	}
}

var wsRe = regexp.MustCompile(`DevTools listening on (ws://\S+)`)

// TestCDPAttach plays an e2e script: it starts the browser with a
// debugging port, signs in (a cookie) in its own browser context with its
// own viewport, and leaves its page open. capture --cdp then attaches,
// captures the signed-in page in a tab of that context, and leaves the
// script's page as it was.
func TestCDPAttach(t *testing.T) {
	chrome := needChrome(t)
	var seen atomic.Int32
	base := fixtureServer(t, true, &seen)

	args := []string{"--headless", "--remote-debugging-port=0", "--user-data-dir=" + t.TempDir(), "--no-first-run", "--hide-scrollbars", "about:blank"}
	if os.Getenv("BRANDKIT_NO_SANDBOX") == "1" || os.Geteuid() == 0 {
		args = append(args, "--no-sandbox")
	}
	cmd := exec.Command(chrome, args...)
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	ws := ""
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		if m := wsRe.FindStringSubmatch(sc.Text()); m != nil {
			ws = m[1]
			break
		}
	}
	go func() {
		for sc.Scan() {
		}
	}()
	if ws == "" {
		t.Fatal("the browser printed no DevTools address")
	}

	// The "e2e script": its own browser context, cookie, viewport and page.
	actx, cancelA := remote.NewAllocator(context.Background(), ws)
	defer cancelA()
	bctx, cancelB := chromedp.NewContext(actx)
	defer cancelB()
	if err := chromedp.Do(bctx); err != nil {
		t.Fatal(err)
	}
	e2e, cancelE := chromedp.NewContext(bctx, chromedp.WithNewBrowserContext())
	defer cancelE()
	if err := chromedp.Do(e2e, chromedp.EmulateViewport(1111, 777)); err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(base, "http://")
	if _, err := chromedp.Call(e2e, network.SetCookie, network.SetCookieParams{Name: "session", Value: "ok", Domain: strings.Split(host, ":")[0], Path: "/"}); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Do(e2e, chromedp.Navigate(base+"/app")); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Do(e2e, chromedp.Evaluate[chromedp.Void](`document.querySelector('h1').textContent = 'state set by e2e'`)); err != nil {
		t.Fatal(err)
	}
	before := seen.Load()

	out := t.TempDir()
	rep, err := Run(context.Background(), Options{CDP: ws, Match: "/app", Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if rep.URL != base+"/app" {
		t.Errorf("captured %s", rep.URL)
	}
	if seen.Load() <= before {
		t.Error("the capture tab didn't load the page signed in")
	}
	checkFindings(t, rep)

	state, err := chromedp.Run(e2e, chromedp.Evaluate[[]any](`[innerWidth, innerHeight, document.querySelector('h1').textContent]`))
	if err != nil {
		t.Fatal(err)
	}
	if state[0] != 1111.0 || state[1] != 777.0 || state[2] != "state set by e2e" {
		t.Errorf("the e2e page changed: %v", state)
	}
	targets, err := chromedp.Targets(bctx)
	if err != nil {
		t.Fatal(err)
	}
	pages := 0
	for _, tg := range targets {
		if tg.Type == "page" && strings.HasPrefix(tg.URL, base) {
			pages++
		}
	}
	if pages != 1 {
		t.Errorf("%d app pages open after the capture, want only the e2e page", pages)
	}
	raw, _ := os.ReadFile(filepath.Join(out, "report.json"))
	var back Report
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
}

// TestStatusAndRedirect: a page that answers 4xx/5xx is an error, not a
// clean report; a redirect is recorded (R1-M9).
func TestStatusAndRedirect(t *testing.T) {
	needChrome(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><html lang="en"><title>ok</title><main><p>Fine.</p></main></html>`))
	})
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusFound) })
	mux.HandleFunc("/", http.NotFound)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for _, p := range []string{"/nope-typo", "/also/missing"} {
		if _, err := Run(context.Background(), Options{URL: srv.URL + p, Out: t.TempDir(), Widths: []int{375}}); err == nil || !strings.Contains(err.Error(), "404") {
			t.Errorf("%s: %v", p, err)
		}
	}
	rep, err := Run(context.Background(), Options{URL: srv.URL + "/old", Out: t.TempDir(), Widths: []int{375}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != 200 || rep.FinalURL != srv.URL+"/ok" {
		t.Errorf("status %d, final URL %q", rep.Status, rep.FinalURL)
	}
}

// TestContrastSkipsInactiveControls: disabled and inert controls are
// exempt from WCAG 1.4.3; an opaque oklch() background ends the background
// walk; a fade-in is checked at its end state (change 2, R1-m8).
func TestContrastSkipsInactiveControls(t *testing.T) {
	needChrome(t)
	page := `<!doctype html><html lang="en"><head><title>c</title><style>
body { background: #f8fafd; color: #aeb9c4; font: 16px sans-serif; }
button, input { background: #f8fafd; color: #aeb9c4; border: 0; font: inherit; }
@keyframes in { from { opacity: 0; } to { opacity: 1; } }
.fade { opacity: 0; animation: in 0.2s 0.1s forwards; }
</style></head><body><main>
<button>Enabled control</button>
<button disabled>Disabled control</button>
<span role="button" aria-disabled="true">Aria disabled</span>
<div inert><p>Inert text</p></div>
<fieldset disabled><legend>Legend text</legend><p>Fieldset text</p></fieldset>
<div style="background-image: linear-gradient(#000, #000)"><p style="background: oklch(0.98 0.005 250)">Opaque oklch</p></div>
<p class="fade">Faded in</p>
</main></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(page)) }))
	defer srv.Close()
	rep, err := Run(context.Background(), Options{URL: srv.URL + "/", Out: t.TempDir(), Widths: []int{375}})
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Widths[0].Contrast
	failed := map[string]bool{}
	for _, f := range c.Failures {
		failed[f.Text] = true
	}
	for _, text := range []string{"Enabled control", "Legend text", "Opaque oklch", "Faded in"} {
		if !failed[text] {
			t.Errorf("%q should fail contrast: %+v", text, c)
		}
	}
	for _, text := range []string{"Disabled control", "Aria disabled", "Inert text", "Fieldset text"} {
		if failed[text] {
			t.Errorf("%q is an inactive control's text, exempt from 1.4.3", text)
		}
	}
	if c.Inactive != 4 {
		t.Errorf("inactive %d, want 4", c.Inactive)
	}
}
