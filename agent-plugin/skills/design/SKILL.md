---
name: design
description: Design web UI inside the project's installed brand package - user stories, three full mockups in the project's own stack (a conventional one and two that each change one named axis), each captured and linted clean before anyone sees it, a pick by the user, the build, and a screenshot review by a fresh reviewer. Use when building a new screen or page, or redesigning one, in a project that has (or should have) a brandkit package. Not for a one-line style fix.
---

# Design

Design inside the brand, never around it. The tokens, themes and rules come from the project's installed brand package; this skill decides layout, hierarchy, density and copy within them, and proves each mockup with `brandkit capture` and `brandkit lint` before the user sees it. Checks that can be code are code: when a step says to run something, run it, don't judge it by eye.

## 0. Scope

- **A new screen or page, or a redesign:** the whole process below.
- **A small addition inside an existing screen** (a field, a state, a dialog): it inherits that screen. Skip the mockups and the pick (steps 3 and 5); do steps 1, 2, 4 and 6 on the change itself.
- **Several screens:** group them by pattern (app shell, list and detail, settings and forms) and run one pick per pattern, not one per screen.

## 1. Intent and stories

Write down, before any markup:

- what the screen is for and who uses it, in the user's words, and how often they use it;
- three to six user stories, each with an acceptance test that a screenshot can show ("As a rep, I see who to call next without scrolling at 375px");
- the data it shows: a realistic set (real-looking names, amounts, dates, statuses; never lorem ipsum) and a worst case (see [references/stress.md](references/stress.md)).

Take these from the plan or issue when one exists; ask only what they don't answer.

## 2. Read the brand package

`brandkit.lock` at the project root names the installed brand and its folder (default `vendor/brand/<name>/`). Read, in that folder:

- `README.md`: the brand sheet, including its do and don't;
- the generated CSS in `export/ui/` (`tokens.css`, `brand.css`, the DaisyUI theme files) and `lint.json`;
- the theme names and which one is the default.

**No package, no design.** If there is no `brandkit.lock`, or it names nothing, stop and say so: the brand has to be defined first, with the `brand` skill and the user. Don't derive a palette from the existing CSS, a screenshot or the product's category.

Then read the project: the stack (templates, CSS pipeline, component library, how DaisyUI is loaded), two or three existing screens, and the components and helpers they share. **Reuse first:** an existing component or token that can express the design is used, even when restyling or rewiring it is more work than writing new markup. Add a component only where none fits, and say so in the hand-off.

Make sure `brandkit version` works (install with `mise use github:shermanhuman/brandkit` if not) and that `brandkit capture` can find a browser (brandkit's README, *Install*).

## 3. Three mockups

**Mode first,** from the surface, not the product:

- **Operate** (app UI: dashboards, lists, forms, settings): scanability, consistency with the rest of the product and familiar patterns come before expression. Density fits how often it's used. The brand shows in precise details: type, spacing, the accent used sparingly.
- **Persuade** (marketing and landing pages): one bold idea, placed once, grounded in what the product actually is. Everything else stays quiet.

**Then three full mockups, all inside the tokens:**

- **A, conventional:** the standard pattern for this kind of screen in this product. For a redesign, A is today's screen improved, so the user can always pick "the same, done properly".
- **B and C each change one named axis from A:** layout (where things sit, what leads), density (how much fits; spacing and type steps), or navigation (how the user moves between items and sections). Name the axis and the direction ("C: density, compact table instead of cards"). Colour, fonts and radius are the brand's and are never an axis.
- Sketch each as a short ASCII wireframe first. If two sketches turn out to be the same idea, change one before building.

Build each as working code in the project's real stack and templates, with the realistic data and a switch to the worst case (for example `?state=worst|empty|error|loading`) and to each theme (for example `?theme=<name>`). Put them where production doesn't serve them (a dev-only route or a `mockups/` folder) and don't change production code yet. Every visible control either works or is plainly placeholder data; no dead buttons that look live.

**Mark your pick** with one line on why it best serves the stories.

Use the short lists while building: [references/avoid.md](references/avoid.md) (looks to avoid), [references/copy.md](references/copy.md) (interface copy), [references/motion.md](references/motion.md) (motion).

## 4. Capture and lint each, until clean

For each mockup, before the user sees any of them:

```sh
brandkit lint <the mockup's templates and CSS> [--compiled <built css>]
brandkit capture --url "<mockup url>?theme=<name>" --out captures/<A|B|C>/<name>
brandkit capture --url "<mockup url>?state=worst" --widths 320,375,640,1280,1920 --out captures/<A|B|C>/worst
```

Capture every theme, and the worst case at the stress widths. Clean means lint reports 0 errors and 0 warnings (lint exits 0 on warnings alone, so read its last line) and each `report.json` summary shows `axeSeriousCritical: 0`, `contrastFailures: 0` and no `overflowWidths`. Fix the mockup (never the tokens) and re-run until it is clean. A lint finding that is genuinely wrong gets `brand-lint-ignore R<n>: <reason>` on that line, and the ignore is listed in that option's checks.

Then look at the screenshots yourself (`w1280.png`, `w375.png` and the worst-case set) against each story's acceptance test, and fix what the numbers can't see: clipped text, a primary action below the fold at 375px, a hierarchy that doesn't match the story's order. Run the captures again after each fix.

## 5. The pick

Put the three in front of the user as one decision: one option at a time at full width, with a fixed A/B/C switcher, your pick opening first and marked ★, desktop (1280) and mobile (375) captures, its checks, and one line each. The mockups are shown as **captures, never as live mockup code** outside a sandboxed frame. [references/pick.md](references/pick.md) has the two ways to post it: a decision board item when the `sugar` CLI is installed and reachable, otherwise a decision page under the host's rules.

Wait for the answer:

- **A letter:** build that one, with any note it carries.
- **More discussion, or a note asking for changes:** rework the mockups the note names, re-run step 4, and post one follow-up pick.

**No user in the session** (a headless or benchmark run): still make the three mockups and run step 4 on each, then take your marked pick on to step 6 (when the deliverable is a standalone page, the picked mockup is that page), and say in your final report that the pick was yours because no one could be asked, with each option's axis and checks.

## 6. Build and review

**Build the pick** into production code following the project's conventions (file layout, naming, components, the brand's classes and variables), then delete the mockup surface. The brand's edit hook warns about raw values as you go; fix each warning before moving on. Finish with the same checks on the real screen:

- `brandkit lint` on the changed files (0 errors, 0 warnings);
- `brandkit capture` of the real screen in each theme and in the worst case; for a signed-in screen, attach to the browser the project's e2e script drives with `--cdp <ws url> --match <path>`;
- the project's own tests.

**Then a fresh reviewer** (a new subagent or process that never saw the build, never a fork of this session) compares the built screen with the stories, the picked mockup and the brand. The brief and the report shape are in [references/review.md](references/review.md). Fix its must-fix findings, capture again, and have a fresh reviewer check only those fixes. Unattended, stop after that one round and report what's left. Post the review on the pull request with the captures.

## Hand-off

Say where to see it: the URL or route, the themes and states to try, and the captures. Mention any component you added and any `brand-lint-ignore` you left, with its reason.
