# Posting the pick

Read this at step 5 of the design skill. Either way, the user sees one option at a time at full width, a fixed A/B/C switcher, the recommended option first and marked ★, desktop and mobile captures, the checks, and one line per option.

## On a decision board (`sugar`)

Use this when `sugar` is on PATH. If any `sugar` command exits 3, the board is unreachable: use the page below instead, and never read the failure as an answer.

1. Upload each option's two captures; each command prints a blob id:

   ```sh
   sugar image captures/A/<default theme>/w1280.png
   sugar image captures/A/<default theme>/w375.png
   ```

   `sugar help` lists the commands if this one has moved.

2. Write one decision item (`sugar schema` prints the full shape) and post it with `sugar post pick.json`, which prints the item's id and URL:

   ```json
   {
     "kind": "decision",
     "title": "Campaigns list: which layout?",
     "minutes": 2,
     "unblocks": "building the campaigns list",
     "recommendation": "B: density, a compact table that fits 25 rows at 1280",
     "summary": "Three mockups inside the brand. A is today's list improved; B and C each change one axis.",
     "options": [
       {
         "id": "A",
         "label": "Conventional: today's card list, with the filters moved above it",
         "blocks": [
           {"type": "image", "blob": "<id>", "alt": "desktop 1280", "caption": "Desktop, 1280"},
           {"type": "image", "blob": "<id>", "alt": "mobile 375", "caption": "Mobile, 375"},
           {"type": "table",
            "columns": [{"name": "Check", "measures": "which check ran"},
                        {"name": "Result", "measures": "pass, or the failures found"}],
            "rows": [["axe", "0 serious or critical"], ["contrast", "all pairs pass"],
                     ["overflow", "none at 320 to 1920"], ["brand lint", "0 findings"]]}
         ]
       }
     ]
   }
   ```

   B (`"label": "Density: a compact table, 25 rows at 1280", "recommended": true`) and C (`"label": "Navigation: the list on the left, the selected campaign on the right"`) follow with the same three blocks. Exactly one option is `recommended`; the board opens it first and adds More discussion and a note field itself. A `mockup` block (HTML in a sandboxed frame) may stand in for an image when the interaction is the point; captures stay the default.

3. Wait with `sugar wait <id>` (default timeout 30 minutes; on timeout, carry on with other work and check later with `sugar get <id>`). The answer is `answers[0].choice`: an option id, or `"more"` with the user's note in `answers[0].note`.

## On a decision page

Without the board, make a decision page where the host's rules say packets go (a packet server, an artifact, or a local HTML file outside the working tree), and give the user its link.

- One option visible at a time, full width, with a fixed switcher (A, B, C; keys 1 to 3) styled as plain page chrome so it doesn't read as part of the design. The recommended option opens first.
- Each option: its label and axis, the 1280 and 375 captures as `<img>`, and the checks as an HTML table whose columns say what they measure.
- **Never put the mockup's live code on the page unsandboxed.** A page served from the same origin as the answer form could post answers itself. If live code is needed, use `<iframe sandbox="allow-scripts">` (no `allow-same-origin`).
- Answers come back the way the host's page delivers them; otherwise ask for a reply in chat: `B`, `B: <note>`, or `more: <what's missing>`.
