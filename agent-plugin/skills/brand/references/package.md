# Package layout and token choices

Read this while drafting (step 2 of the brand skill). Field-by-field meanings are in [brandkit's README](https://github.com/shermanhuman/brandkit#a-brand-package); this file covers only the choices the skill makes.

## Files

```
brandkit.json                 name, description, repo, resolver, themes, fonts, install
tokens/primitives.tokens.json raw palette, font families, type, spacing and radius scales
tokens/semantic.tokens.json   roles that alias primitives; the rules in root $extensions
tokens/<theme>.tokens.json    one per theme context: surfaces, content colours, role overrides
tokens/<name>.resolver.json   sets (primitives, semantic) and the theme modifier
assets/                       mark and lockups as SVG, one per theme where they differ
fonts/                        WOFF2 files, @font-face CSS and each licence
export/, README.md            written by brandkit build; commit them, never edit by hand
```

Put `tokens`, `fonts` and `assets` in `install` when other brands will extend this one or projects need the fonts and logos. A brand that extends another also lists `vendor`, which holds the installed base.

## Naming

- **Primitives** describe the value (`color.violet.600`, `space.4`), **roles** describe the use (`color.surface.base`, `color.text.muted`). Projects and the DaisyUI mapping refer to roles only, so a palette change never renames anything.
- Map all 20 DaisyUI colours (`base-100` … `error-content`) to roles, even when two share a value. A role a theme doesn't change lives in the semantic file, not in each theme file.
- Keep the source's names where one source dominates and they already follow this split; renaming costs every consumer a migration.

## Values

- Type: one family per role (body, display, mono) and the scale largest first. Sizes in rem.
- Spacing and radius: a short scale (five to eight steps) taken from what the sources actually use; collect the values from their CSS rather than inventing a progression.
- Corners: rounded (the radius scale, mapped to DaisyUI's `radius-selector`, `radius-field`, `radius-box`). A brand whose look is pixel art can add pixel-stepped corners for its marketing pages only; product interfaces stay smooth. Then declare `corners` `{"style": "pixel", "small": 2, "box": 4, "focus": "<the focus ring's colour token>", "pages": ["<marketing page source globs>"]}` in the rules: the themes keep the radius, and `brand.css` cuts every corner inside the scope (`[data-corners="pixel"]` on a marketing page's `<html>` or `<body>`, or the `scope` you name) into a two-step pixel staircase, with the outline and the focus ring redrawn on the steps. `pages` are globs relative to the consuming project's root (`site/**`); lint R10 flags smooth corners only there, and is off without them. Which style, the step sizes and the globs are the user's call; show both looks in the packet as rendered components, not words.
- Brand arc: when the sources put every chromatic colour on one hue path in equal OKLCH steps (the Breakdown brands: 66° − 43.7° × n), declare `arc` with its `start`, signed `step`, and each deliberate exception with its reason, so `check` warns when a colour drifts off it. When adding a colour to a brand with an arc, pick the free stop nearest the hue it needs (the sheet's Brand arc table lists the free stops), then match the weight of the colours beside it; only a colour that must sit elsewhere becomes an exception, and that is the user's call.
- Contrast pairs: declare every text-on-surface pair the UI uses, each with its minimum (4.5 for body text, 3 for large text and UI parts) and a one-line use. `check` also tests every DaisyUI colour under its `-content` colour, and `base-content` on `base-100`, in each theme at 4.5:1 without being told; declare those only to change the minimum (`daisyui:accent-content` on `daisyui:accent`, `min` 3 for a large-type-only accent). A pair whose tokens differ by theme can name `daisyui:<colour>` instead of a helper token.

## The conflict and gap list

Keep it in the working notes and the packet, in this shape:

| # | What | Source A | Source B | Recommended | Why |
|---|---|---|---|---|---|
| 1 | `accent` (night) | guide `#7c3aed` | admin CSS `#8b5cf6` | guide | the spec; admin drifted in a copy |
| 2 | night `warning` | none | none | proposal: `oklch(0.80 0.15 75)` | derived from the day hue to pass 4.5:1 on `base-100` |

A gap's "source" columns say `none`; its recommendation is labelled a proposal.
