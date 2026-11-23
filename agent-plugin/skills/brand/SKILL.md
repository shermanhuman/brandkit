---
name: brand
description: Define or update a brand as a brandkit package (DTCG tokens, themes and checkable rules) with the user. Use when a brand needs codifying, when its logos, CSS or guides disagree, when a brand package needs changes, or when UI work needs a brand package and none exists yet. It inventories the sources, settles every conflict in one decision packet, and ends with an approved README brand sheet.
---

# Brand

Turn a brand into a brandkit package: DTCG 2025.10 token files, a resolver with one context per theme, and the brand's checkable rules, from which `brandkit build` writes the CSS, both DaisyUI theme forms, the lint config and a README brand sheet. The user decides what the brand is; you find, compare and propose. **Never invent a palette, a font or a logo.** Every value comes from an existing source or from the user's answer.

Package field names and the extension keys are in [brandkit's README](https://github.com/shermanhuman/brandkit#a-brand-package); for command flags use `brandkit help`, which always matches the installed binary. The fixture brands [`base`](https://github.com/shermanhuman/brandkit/tree/main/testdata/brands/base) and [`sub`](https://github.com/shermanhuman/brandkit/tree/main/testdata/brands/sub) (which extends `base`) are complete packages to copy the shape from. [references/package.md](references/package.md) covers the layout and the choices this skill makes within it.

## 0. Tool

`brandkit version` must work. If it doesn't, install it with `mise use github:shermanhuman/brandkit`. `build` needs a browser for the README specimens (the README's *Install* section). `--no-images` only reuses specimens an earlier build wrote, so the first build of a package needs the browser.

## 1. Inventory every source

Find everything that already says what the brand looks like, before drafting anything. Search the repositories the user named, their sites and apps, and any brand or design folder:

- **Logos and marks:** SVG and PNG lockups, favicons, app icons, social images. Note which have a vector master and which exist only as bitmaps.
- **Colour and type in code:** `:root` custom properties, DaisyUI theme blocks (`@plugin "…daisyui-theme"` or `[data-theme]`), Tailwind config or `@theme`, token JSON of any format, hard-coded values that recur in stylesheets.
- **Written rules:** brand guides, design-token docs, READMEs, drafts, decks. A draft counts as a source; say it's a draft.
- **Fonts:** families, the files actually served, and their licences.
- **Copies:** the same theme copied into several projects. Diff them against each other; copies drift.

Record each source in one table: where it is (path and commit, or URL), what it holds, and how current it looks (last change date). Then list **every conflict**: one row per value that differs between sources, with each source's value side by side (`accent`: guide `#7c3aed`, admin app `#8b5cf6`, site `#7c3aed`). A conflict you don't list is a conflict you decided silently.

Then list the **gaps**: what the package needs and no source gives, such as a missing night or day theme, a role with no colour (`warning`, `info`), content colours that fail contrast, no type scale, no spacing or radius scale, no vector logo, a font with no licence file.

## 2. Draft the package

Work in the package's own folder or repository. Where it lives (a new repository, its name, public or private) is the user's call: if they haven't said, it goes in the packet.

1. **`brandkit.json`**: name, description, `repo`, the resolver, the themes (one `default`), fonts and what `install` ships.
2. **Tokens**, split as the fixtures split them: primitives (the raw palette, font families, scales), semantic roles that alias primitives, and one file per theme context. Colours are OKLCH with the source's displayed hex kept in the `hex` field, so a consumer that reads hex sees exactly what it saw before. Two sources whose hex values differ at all are a conflict, however close they look.
3. **Resolver:** one set per layer and a `theme` modifier with one context per theme.
4. **Extending another brand:** run `brandkit install owner/base@tag` in this package (the files land in `vendor/brand/<base>/`, pinned in `brandkit.lock`; the base's `install` list must include `tokens`) and list the vendored token files first in each resolver set and context. This brand's own files come after them and win. Override only what differs; never copy the base's values into this package. Add `vendor` to this package's `install` list, so projects that install it also get the base's fonts and assets. (The `sub` fixture refers to `vendor/fixture-base/` because it vendors by hand; an installed base is under `vendor/brand/`.)
5. **Rules** in the token files' root `$extensions` (the keys are in the README's *Token extensions*): the DaisyUI role mapping, contrast pairs with their minimum and use, the accent and the body-text tokens it may never be, shadows, the mark and lockup per theme, clear space, the icon set, the scales, do and don't.
6. Fill each value from the source the conflict table favours. Set every value still open to the string `"TODO"`, the open-value marker: `check` fails on any `"TODO"`, so nothing unsettled can ship.

So `check` is what catches a `"TODO"`; `build` may still write a sheet that shows one, so don't use the draft's sheet; the previews in step 3 are how you check the rest.

## 3. One packet to the user

Settle every conflict and gap in one round trip, as one decision packet: follow the host's rules for decision packets where it has them (for example an `interrogate` skill), otherwise a local HTML page outside the working tree, with answers in chat.

- **Show, don't describe.** Colour conflicts as swatches side by side with each pairing's contrast ratio; type as a rendered specimen; logos as images in both themes.
- **Previews come from candidate packages,** for decisions that change a package value (not, say, where the repository lives). For each such option, copy the draft to a scratch folder outside the working tree, fill that decision's open values with the option's values and every other open value with its recommendation, then run `brandkit check <dir>` and `brandkit build <dir>` there. Use that build's sheet images, take the ratios from its Contrast table, and put any `check` failure in that option's text. The real package is built only after the answers (step 4). If an open value has no option to fill it with, ask about it in the packet without a preview, showing the value a source holds where one does. The other candidates still hold its `"TODO"`: use their sheets where `build` writes one, and expect that one `check` failure.
- **Options from the sources first.** Each conflict's options are the values the sources hold. Where a gap has no source, you may propose values derived from what the brand already has (a content colour computed to pass 4.5:1 on an existing surface, a night surface from the day hue), labelled as proposals with how you derived them. A brand with no colours at all gets a question, not a proposed palette.
- **Recommend one option** for each, with the reason in one line (most-used copy, the newest source, the only one that passes contrast). Every decision ends with More discussion.
- **Critical first:** the repository's home and visibility, the logo master, anything that changes what existing products show. Then the rest, each with your default.
- Mechanical choices (file layout, token names, scale steps that follow from a decided base) are decided without asking; list them as "decided unless you object".

Wait for the answers. Record them where the plan or issue for this work lives, with the date. A More discussion answer gets one follow-up packet that shows what was missing; never fall back to one question at a time.

With no user in the session (a headless or scheduled run), stop here: write the packet, report where it is, and don't build a package on your own recommendations.

## 4. Build and check

Apply the answers, then:

```sh
brandkit build        # writes export/ and README.md; commit both
brandkit check        # schema, contrast of every declared pair and DaisyUI -content pair, brand rules
```

Step 4 is done when every row of the conflict and gap list has a recorded answer, no `"TODO"` is left, and `check` reports no errors. `check` exits 0 with warnings (no lockup, no icon set, a font the package doesn't ship), so read them: each is either fixed or a gap with a recorded answer. A contrast failure is fixed by moving the failing colour along its own hue in OKLCH, or by pairing it with a different content colour. If the fix visibly changes a colour the user chose, show them the before and after and ask; don't ship a silent change to their answer.

If the package replaces an older token file that projects still read, generate that file too (`tokensCss.template` reproduces a legacy layout byte for byte) and diff it against the old one. Any difference is a change those projects will see: list it.

## 5. Approve the sheet

Push the branch so the host renders the generated `README.md`, and send the user its link with one line per section: logos and lockups per theme, clear space, colour roles with contrast, type, spacing and radius, icons, component examples, do and don't, install. Ask them to look at it in both the host's light and dark modes, since the sheet swaps its images by colour scheme.

Their changes go back through step 4. When they approve, the release (a tag that projects pin with `brandkit install owner/repo@tag`) follows the host's release rules; tagging is usually the user's.

## Updating an existing package

Read the package and its sheet first. The same rules apply at a smaller scale: list what changes and what it affects, ask in one packet, build, check, and show the sheet's changed sections. A change to a base brand reaches every brand that extends it at their next `brandkit update`: name them.
