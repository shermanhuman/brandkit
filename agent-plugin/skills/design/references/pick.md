# Laying out the pick

Read this at step 5 of the design skill. The pick goes where the host's rules send questions for the user (a decision board, a packet page, an artifact); with no such rule, a local HTML file outside the working tree, with the answer in chat. Whatever the surface, the user sees:

- **One option at a time, at full width,** with a fixed A/B/C switcher (keys 1 to 3 where the page allows) styled as plain page chrome, so it doesn't read as part of the design.
- **Your pick opens first,** marked ★.
- **For each option:** its letter, a one-line label naming the axis and direction ("B: density, a compact table that fits 25 rows at 1280"), the 1280 and 375 captures of the default theme, and its checks as a table whose columns say what they measure:

  | Check | Result |
  |---|---|
  | axe | 0 serious or critical |
  | contrast | all pairs pass (disabled controls exempt) |
  | overflow | none at 320 to 1920 |
  | brand lint | 0 findings in 6 files; JSX sources not read, rendered HTML checked |

  Anything still open after the three fix rounds, and any `brand-lint-ignore`, is a row of its own.
- **More discussion** as the last choice, with room for a note.

**Never put a mockup's live code on the page unsandboxed.** Code served from the same origin as the answer form could post answers itself. When the interaction is the point, use `<iframe sandbox="allow-scripts">` (no `allow-same-origin`); captures stay the default.

Several picks for one goal (one per pattern) go out together as one group, so the user answers them in one sitting.

If the answer doesn't come back through the page, ask for a reply in chat: `B`, `B: <note>`, or `more: <what's missing>`.
