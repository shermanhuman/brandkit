# Stress cases

Every mockup and the final build get the worst case, not just the happy data. Put the cases behind the mockup's state switch (for example `?state=worst`) so `brandkit capture` can reach each one.

## Widths and zoom

| Case | How to capture | Look for |
|---|---|---|
| Narrow phone | `--widths 320,375` | horizontal overflow, clipped buttons, tables that need their own scroll |
| 200% zoom | `--widths 640` (a 1280 window at 200% lays out at 640 CSS px) | text cut off, sticky headers covering half the screen |
| Desktop and wide | `--widths 1280,1920` | lines over about 80 characters, content stranded in a corner |

`capture` reports overflow per width; the rest is in the screenshots.

## Content

- **Long strings:** a 60-character name, an unbroken URL or email, a label about 40% longer than English (as German often is). They wrap or truncate with the full value available (a title attribute or the detail view), never overflow.
- **Big and odd numbers:** 1,234,567.89, negatives, zero, a missing value shown as "—" with its reason when there is one.
- **Counts:** none, one, a page's worth, and 1,000 or more (does it paginate or virtualise; does a count badge fit).
- **States:** empty (first use, and filtered to nothing), loading, error (a failed load, and a failed save with the user's input kept), and partial (some rows failed).
- **Permissions:** a user who can see but not edit; the controls they can't use are absent or plainly disabled with the reason.

## Themes and input

- **Every theme** the package defines: capture each (`?theme=<name>`); contrast that passes in one theme can fail in the other.
- **Keyboard:** every control reachable with Tab in a sensible order, with a visible focus ring (axe in `capture` checks names and roles, not the order).
- **Right to left,** when the product ships an RTL language: the layout mirrors and icons that imply direction flip.

---

Adapted from the `break-ui` skill in Emil Kowalski's skills (MIT, [emilkowalski/skills](https://github.com/emilkowalski/skills), `64eb3de`, 2026-11-15). Reworded and extended; not copied.
