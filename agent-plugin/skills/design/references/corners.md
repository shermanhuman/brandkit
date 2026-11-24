# Pixel corners

Read this when the brand's `lint.json` has `"corners": "pixel"`. The brand cuts corners into pixel steps on marketing pages only; product UI keeps the smooth radius. The mechanics are in [brandkit's README](https://github.com/shermanhuman/brandkit#pixel-corners).

**On a marketing page:**

- Put the scope (`data-corners="pixel"`, unless the brand's `brand.css` names another) on `<html>` or `<body>`, and load `brand.css` after the DaisyUI theme.
- Leave DaisyUI's components to `brand.css`. On your own boxes, use `px-corners-sm` (controls) or `px-corners-lg` (panels). Never add `rounded-*` or `border-radius`.
- A custom box with a border other than Tailwind's `border`, `border-2`, `border-4` or `border-8` sets `--px-bw` to its width, or its steps lose their outline.
- `clip-path` cuts whatever hangs outside a stepped box. Keep tooltips, dropdowns and other popovers outside stepped boxes such as cards, and capture their open state: hover and open states aren't in a default capture.
- Check that the page's sources match a `cornerPages` glob in `lint.json`. If none does, R10 isn't reading them: say so in the option's checks and report it as a package gap for the `brand` skill.

**In product UI:** use the radius as usual and never the scope.
