# Looks to avoid

Defaults that make a screen read as generated rather than designed. Check each mockup against this list at step 3. The brand package wins: if its sheet's do list asks for one of these, it isn't a fault.

| Look | What it is | Why it hurts |
|---|---|---|
| The SaaS card kit | every item in an identical rounded card with a soft drop shadow, often on a gradient wash | it hides which item matters; cards inside cards waste space on mobile |
| Template chrome | ALL-CAPS eyebrow labels over headings, italic accent words inside headlines, `A · B · C` meta strings, `→` on every button, monospace for labels that aren't code | decoration that encodes nothing, and the same on every generated page |
| Fake sequence | `01 / 02 / 03` numbering on items that aren't steps | numbering promises an order the content doesn't have |
| Stat hero | a big number with three small stats as the page opener | it fits only screens whose job is that number |
| Pills everywhere | every button and badge fully rounded | pill buttons and pill badges stop being distinguishable; use the brand's radius scale, or on marketing pages its pixel corners when it has them |
| Icon tile over heading | a coloured square holding an icon, stacked above each card's heading | it repeats the heading and pushes content down |
| Side-stripe accent | a thick coloured left border marking cards or alerts | it reads as a status that isn't there; use the brand's alert roles |
| Gradient text and glows | gradient-filled headings, glowing shadows on buttons or cards | they fail contrast checks and fight a brand's flat surfaces |
| Grey on colour | muted grey text on a coloured surface | it fails contrast; use that surface's content colour |
| Faint text | body or label text at 40–60% opacity | it fails contrast in one theme or the other; use a muted role |
| Motion as decoration | each section fading or sliding in on load, every card lifting on hover | it slows the page and says nothing; see [motion.md](motion.md) |
| A second design system | new colour, spacing or font values written next to the brand's | the brand drifts; lint finds these, and the fix is the brand's token, not an ignore |

Spend boldness in one place per screen (in Persuade mode, the one idea; in Operate mode, usually none). Visual structure should encode something true: a divider, a number or a heading level means a real grouping, order or rank.

---

Adapted from Anthropic's `frontend-design` skill (Apache 2.0, [anthropics/skills](https://github.com/anthropics/skills/tree/main/skills/frontend-design), 2026-09-03) and the pattern names in impeccable's detector (Apache 2.0, [pbakaus/impeccable](https://github.com/pbakaus/impeccable), v4.10.0). Reworded and extended; not copied.
