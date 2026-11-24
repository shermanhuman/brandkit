# Motion

Use the brand's motion tokens (durations, easing) from `brand.css` when it has them; these numbers apply where it doesn't.

- **Motion answers an action** (opening, closing, adding, removing, moving between states). Nothing animates on page load just to arrive.
- **Not on frequent actions:** anything done many times a day (keyboard shortcuts, the command palette, switching tabs, typing) gets no animation.
- **Under 300 ms** for UI motion; 150–250 ms is typical. Modals, drawers, sheets and toasts may take a little longer.
- **Ease-out for things entering, never ease-in** for UI (it feels late). For example `cubic-bezier(0.23, 1, 0.32, 1)`.
- **Transform and opacity only;** animating width, height, top or left causes layout work and jank.
- **Respect `prefers-reduced-motion`:** replace movement with an instant change or a plain fade.
- **No `animation-fill-mode: forwards` with a transform on a container** that holds fixed-position children (a toolbar, a floating button): the leftover transform makes them scroll with the page.
- `brandkit capture` freezes animations, so check each end state as its own capture (`?state=open`), not mid-transition.

---

Adapted from Emil Kowalski's skills (MIT, [emilkowalski/skills](https://github.com/emilkowalski/skills), `64eb3de`, 2026-11-15), including the fill-mode bug reported in its issue #46. Reworded; not copied.
