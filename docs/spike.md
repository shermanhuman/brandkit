# Spike: browser, Tailwind and `--cdp` (forge#30, Step 1)

Run on 2026-11-22 on soulglow (Linux x86-64), mise 2026.10.15, Go 1.27.4, chromedp v0.24.0. All three parts passed, so the build went ahead on them unchanged.

## (a) chrome-headless-shell pinned through mise, driven by chromedp: passed

`mise.toml` pins Chrome for Testing's headless shell with the `http:` backend, one URL and sha256 per platform:

```toml
[tools."http:chrome-headless-shell"]
version = "158.0.8127.36"
bin_path = "."
strip_components = 1
[tools."http:chrome-headless-shell".platforms]
linux-x64 = { url = "https://storage.googleapis.com/chrome-for-testing-public/{{ version }}/linux64/chrome-headless-shell-linux64.zip", checksum = "sha256:2d3a2301…" }
macos-arm64 = { … }
macos-x64 = { … }
```

- `mise install` fetched and verified the 132 MB zip in about 4 s. `mise which chrome-headless-shell` gives the binary.
- chromedp (`NewExecAllocator` + `ExecPath`) launched it over the pipe. `navigator.userAgent` read `HeadlessChrome/158.0.8127.36`, a computed style read back as `rgb(255, 0, 0)`, and `FullScreenshot` returned a PNG.
- **Limit:** Chrome for Testing has no linux-arm64 build. The `brandkit` binary is still built for linux/arm64, but `capture` and the README specimens there need a Chromium passed with `--chrome` or `BRANDKIT_CHROME`. How `mise install` behaves on linux-arm64 with no URL for the platform wasn't tested (no such machine here).

## (b) Tailwind 4 standalone CLI + vendored DaisyUI with `--color-*: initial`: passed

Tailwind 4.3.5 comes from mise's `github:tailwindlabs/tailwindcss` backend (mise has no registry entry). For DaisyUI 5.8.2 and 5.0.35 (the release assets `daisyui.js` and `daisyui-theme.js`), a stylesheet of

```css
@import "tailwindcss" source(none);
@source "../index.html";
@plugin "../vendor/daisyui" { themes: false; }
@plugin "../vendor/daisyui-theme" { name: "night"; default: true; … --color-primary: oklch(0.6 0.2 300); … }
@theme { --color-*: initial; --color-white: #fff; --color-transparent: transparent; }
```

built against markup using `bg-primary btn btn-primary alert alert-error bg-base-200 text-base-content bg-blue-500 text-red-600 bg-white text-black border-transparent` gave, on both versions:

| class | emitted |
|---|---|
| `bg-primary`, `btn-primary`, `alert-error`, `bg-base-200`, `text-base-content` | yes |
| `bg-blue-500`, `text-red-600`, `--color-blue-500` | **no** |
| `bg-white`, `border-transparent` (re-added in `@theme`) | yes |
| `text-black` (not re-added) | no |

Every `var(--color-…)` that the output uses is also defined in it, so DaisyUI's components don't lean on the removed palette. The theme's OKLCH values come through unchanged.

## (c) `--cdp` attach to a Chromium that Playwright launched: passed

A stand-in for an e2e script (`playwright` 1.65.1 installed locally with npm in a scratch folder, Node 26 from mise) launched Chromium with `args: ['--remote-debugging-port=9333']` (Playwright still uses its own pipe as well), made a context with a 1111×777 viewport and a session cookie, opened a page that answers 401 without the cookie, and changed the page's DOM. A Go program then:

1. attached with `remote.NewAllocator(ws)` (the `webSocketDebuggerUrl` from `/json/version`);
2. listed targets, found the e2e page by URL and read its `browserContextId`;
3. opened its own target in that context (`chromedp.WithExistingBrowserContext`) and loaded the same URL; it was signed in (the cookie is shared);
4. captured at 375, 768, 1280 and 1920 with `EmulateViewport`, then `ResetViewport`, and closed its tab.

Afterwards the e2e page still had its 1111×777 viewport and its DOM change, the context had one page again, and the script carried on (a reload worked). This held both for Playwright's `chromium_headless_shell-1217` and for its full `chromium-1217`.

What this means for the build:
- Our tab shares the context's cookies and storage, not the e2e page's in-memory state. A state that lives only in the page's JavaScript (an open dialog, unsaved input) must be reachable by URL, or the e2e script must open the URL we capture. This is the trade-off the plan chose: touching the e2e page's own viewport would undo Playwright's emulation.
- `FullScreenshot` grows the image to the content width when the page overflows (a 900 px element made the 375 capture 900 px wide). `capture` therefore clips each screenshot to the viewport width and reports overflow separately.
- The `--cdp` fixture test in Go doesn't need Node: it starts the pinned headless shell with a debugging port, sets a cookie in a new browser context over CDP (as the e2e script would), and runs `capture --cdp` against it.
