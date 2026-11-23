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
- Contrast pairs: declare every text-on-surface pair the UI uses, each with its minimum (4.5 for body text, 3 for large text and UI parts) and a one-line use. `check` only checks what is declared.

## The conflict and gap list

Keep it in the working notes and the packet, in this shape:

| # | What | Source A | Source B | Recommended | Why |
|---|---|---|---|---|---|
| 1 | `accent` (night) | guide `#7c3aed` | admin CSS `#8b5cf6` | guide | the spec; admin drifted in a copy |
| 2 | night `warning` | none | none | proposal: `oklch(0.80 0.15 75)` | derived from the day hue to pass 4.5:1 on `base-100` |

A gap's "source" columns say `none`; its recommendation is labelled a proposal.
