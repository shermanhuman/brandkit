# The screenshot review

Read this at step 6 of the design skill, to brief the reviewer. The reviewer is a new subagent or process that never saw the build. A reviewer that inherits the build's transcript inherits its framing and tends to accept what it sees.

## What the reviewer gets

- the user stories with their acceptance tests (step 1);
- the picked mockup's captures and the user's note with the pick, if any;
- the built screen's captures: every theme, the worst case, the stress widths, and each `report.json`;
- `brandkit lint` output for the changed files, including any ignores and their reasons;
- the path to the brand package's `README.md` and its generated CSS;
- the branch or diff, for reading the markup when a screenshot raises a question.

It doesn't get the build transcript, the other mockups, or your own assessment.

## What it does

1. Judges each story's acceptance test from the screenshots first, then from `report.json`, and names the screenshot that shows it.
2. Compares the build with the picked mockup: layout, hierarchy, density and copy. A difference the user's note asked for is correct; any other difference is a finding.
3. Checks the build against the brand: only the package's roles and scales, the sheet's do and don't, and every lint ignore's reason.
4. Checks the stress cases in [stress.md](stress.md) that the captures cover, and lists the ones they don't.

## What it returns

A verdict, **ship** or **fix**, then findings in two groups:

- **Must-fix:** a story that fails, a difference from the pick, a brand violation, a stress case that breaks.
- **Polish:** the rest.

Each finding gives the screenshot or file and line, what's wrong, and the fix. Post the report on the pull request with the captures it cites. After the fixes, a fresh reviewer checks only those fixes against the same inputs.

---

The fresh-context reviewer is adapted from impeccable's finish reviewer (Apache 2.0, [pbakaus/impeccable](https://github.com/pbakaus/impeccable), v4.10.0). Reworded; not copied.
