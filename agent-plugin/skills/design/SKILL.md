---
name: design
description: Design web UI inside a brandkit brand package - a new screen or page, a redesign, or an addition to an existing screen. Use in a project that has `brandkit.lock`, or whose plan or issue says to adopt a brand package. It makes three checked mockups for the user to pick from, builds the pick and has a fresh reviewer check the screenshots. Not for a one-line style fix.
---

# Design

Design inside the brand, never around it. The tokens, themes and rules come from the project's installed brand package; this skill decides layout, hierarchy, density and copy within them, and proves each mockup with `brandkit capture` and `brandkit lint` before the user sees it. Checks that can be code are code: when a step says to run something, run it, don't judge it by eye. For flags, `brandkit help`.

**With no user in the session** (a headless or benchmark run): never stop to ask. Write each assumption down and go on; step 5 says how the pick is made. With no pull request, the review goes in your final report.

## 0. Scope

- **A new screen or page, or a redesign:** the whole process below.
- **A small addition inside an existing screen** (a field, a state, a dialog): it inherits that screen, so there are no mockups and no pick. Do steps 1 and 2, then capture the existing screen in each theme to `<captures>/before/<name>` (with `--cdp` for a signed-in screen) before you change anything. Build the change, then run step 6's checks and review, with those before captures standing in for the picked mockup.
- **Several screens:** group them by pattern (app shell, list and detail, settings and forms) and make one pick per pattern, not one per screen; [references/pick.md](references/pick.md) says how they go out.

## 1. Intent and stories

Write down, before any markup:

- what the screen is for and who uses it, in the user's words, and how often they use it;
- three to six user stories, each with an acceptance test that a screenshot can show ("As a rep, I see who to call next without scrolling at 375px");
- the data it shows: a realistic set (real-looking names, amounts, dates, statuses; never lorem ipsum) and a worst case (see [references/stress.md](references/stress.md)).

Take these from the plan or issue when one exists; ask only what they don't answer.

## 2. Read the brand package

`brandkit.lock` at the project root names the installed brand, its folder (default `vendor/brand/<name>/`) and the path of its `lint.json`. Read, in that folder:

- `README.md` and its sheet images: the brand sheet, including its do and don't;
- the generated CSS in `export/ui/` (`tokens.css`, `brand.css`, the DaisyUI theme files) and `export/lint.json`;
- the theme names and which one is the default.

**No lock:** if the brand's package already exists (the plan, the host's rules or the user say where), run `brandkit install owner/repo@tag`, commit `vendor/brand/` and `brandkit.lock`, and go on. Only when no package exists, stop and say so: the brand has to be defined first, with the `brand` skill and the user. Don't derive a palette from the existing CSS, a screenshot or the product's category.

Then read the project: the stack (templates, CSS pipeline, component library, how DaisyUI is loaded), two or three existing screens, and the components and helpers they share. **Reuse first:** an existing component or token that can express the design is used, even when restyling or rewiring it is more work than writing new markup. Add a component only where none fits, and say so in the hand-off.

Make sure `brandkit version` works (install with `mise use github:shermanhuman/brandkit` if not) and that `brandkit capture` can find a browser (brandkit's README, *Install*).

## 3. Three mockups

**Mode first,** from the surface, not the product:

- **Operate** (app UI: dashboards, lists, forms, settings): scanability, consistency with the rest of the product and familiar patterns come before expression. Density fits how often it's used. The brand shows in precise details: type, spacing, the accent used sparingly.
- **Persuade** (marketing and landing pages): one bold idea, placed once, grounded in what the product actually is. Everything else stays quiet.

**Then three full mockups, all inside the tokens:**

- **A, conventional:** the standard pattern for this kind of screen in this product. For a redesign, A is today's screen improved, so the user can always pick "the same, done properly".
- **B and C each change one named axis from A.** The usual axes are layout (where things sit, what leads), density (how much fits; spacing and type steps) and navigation (how the user moves between items and sections); another is allowed when you name it. Name the axis and the direction ("C: density, compact table instead of cards"). Colour, fonts and radius are the brand's and are never an axis.
- Sketch each as a short ASCII wireframe first. If two sketches turn out to be the same idea, change one before building.

Build each as working code in the project's real stack and templates, with the realistic data and a switch to the worst case (for example `?state=worst|empty|error|loading`) and to each theme (for example `?theme=<name>`). Put them where production doesn't serve them (a dev-only route or a `mockups/` folder) and don't change production code yet. Every visible control either works or is plainly placeholder data; no dead buttons that look live.

**Mark your pick** with one line on why it best serves the stories.

Use the short lists while building: [references/avoid.md](references/avoid.md) (looks to avoid), [references/copy.md](references/copy.md) (interface copy), [references/motion.md](references/motion.md) (motion).

## 4. Capture and lint each

For each mockup, before the user sees any of them, run these from the project root:

```sh
brandkit lint <the mockup's templates and CSS> [--compiled <built css>]
brandkit capture --url "<mockup url>?theme=<name>" --out <captures>/<A|B|C>/<name>
brandkit capture --url "<mockup url>?state=worst" --widths 320,375,640,1280,1920 --out <captures>/<A|B|C>/worst
```

`<captures>` is a folder outside the working tree (or one listed in `.gitignore`); captures are never committed. Capture every theme, and the worst case at the stress widths.

**Clean means:**

- lint's last line names the brand, counts at least one file checked, and shows 0 errors and 0 warnings (lint exits 0 on warnings alone, so read the line);
- each `report.json` summary shows `axeSeriousCritical: 0`, `contrastFailures: 0` and no `overflowWidths`. Disabled and inert controls are exempt from contrast (WCAG 1.4.3), and capture skips them.

**Lint coverage:** lint reads CSS, HTML, HEEx, Elixir `~H` and Go templates, and lists any file it skipped. For any other stack (JSX, TSX, Vue, Svelte), lint the rendered HTML (save the page's HTML from the running mockup) and the built CSS with `--compiled`, and say in that option's checks which source files lint couldn't read. Zero files checked is not clean.

Fix the mockup (never the tokens) and re-run. A lint finding that is genuinely wrong gets `brand-lint-ignore R<n>: <reason>` on that line, and the ignore is listed in that option's checks. **At most three fix rounds per mockup:** whatever is still failing after the third goes in that option's checks with its selector and why it couldn't be fixed. A failure the brand's own roles cause (a pair the package itself fails) is also reported as a package gap for the `brand` skill.

Then look at the screenshots yourself (`w1280.png`, `w375.png` and the worst-case set). First confirm each one shows the theme and state its folder is named for; a switch that silently did nothing passes every number. Then check them against each story's acceptance test, and fix what the numbers can't see: clipped text, a primary action below the fold at 375px, a hierarchy that doesn't match the story's order. Run the captures again after each fix.

## 5. The pick

Put the three in front of the user as one decision, laid out and posted as [references/pick.md](references/pick.md) describes.

The build waits for the answer: it's a taste call, so it never defaults to your pick while a user can be asked. Carry on with other work meanwhile.

- **A letter, with or without a note:** build that option, with the note applied.
- **More discussion with a note:** rework the mockups it names, run step 4 again, and post one follow-up pick.
- **More discussion with no note:** ask once, in plain words, what was missing. A second More discussion moves to conversation until the user wants a pick again.

**No user in the session:** still make the three mockups and run step 4 on each, then take your marked pick on to step 6 (when the deliverable is a standalone page, the picked mockup is that page). Say in your final report that the pick was yours because no one could be asked, with each option's axis and checks.

## 6. Build and review

**Build the pick** into production code following the project's conventions (file layout, naming, components, the brand's classes and variables), then delete the mockup surface. The brand's edit hook warns about raw values as you go; fix each warning before moving on. Finish with step 4's checks on the real screen (lint the changed files), plus:

- for a signed-in screen, `brandkit capture --cdp <ws url> --match <path>`, attached to the browser the project's e2e script drives;
- the project's own tests.

**Then the screenshot review** by a fresh reviewer, briefed as [references/review.md](references/review.md) says. Fix its must-fix findings and capture again, then have a fresh reviewer check only those fixes; repeat while it finds must-fix issues, at most three rounds. Anything still open after that is listed in the hand-off. Post the review on the pull request, attaching the captures it cites the way the host's rules attach images (or naming their paths when the host has no way to).

## Hand-off

Say where to see it: the URL or route, the themes and states to try, and the captures. Mention any component you added, any `brand-lint-ignore` you left with its reason, and any finding still open.
