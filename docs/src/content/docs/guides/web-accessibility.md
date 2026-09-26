---
title: Check accessibility
description: Audit pages against the WCAG rules with axe-core, and check what assistive technology reads on them, in the scenarios that use them.
---

The `web-a11y` pack checks that pages can be used whatever the abilities of the people who use them: that a screen reader can name every field, that text stands out from its background, that images say what they show. It builds on the `web-core` pack, which comes with it: `axx pack add web-a11y` adds both.

## Audit a page

`the page has no accessibility violations` audits the page with [axe-core](https://github.com/dequelabs/axe-core), the accessibility engine of Deque that Lighthouse runs too, against the WCAG 2.1 AA rules:

```gherkin
Scenario: Shops can get a quote whatever their abilities
  When the "/quote" page is opened
  Then the page has no accessibility violations
```

A violation fails the step, which says which rule, how serious it is, and which elements break it, with a link to the rule:

```text
The page has 2 accessibility violations (wcag21aa):
  label (critical): Form elements must have labels; 1 element: #tracking
    https://dequeuniversity.com/rules/axe/4.13/label?application=axx
  color-contrast (serious): Elements must meet minimum color contrast ratio thresholds; 1 element: #note
    https://dequeuniversity.com/rules/axe/4.13/color-contrast?application=axx
```

The audit looks in every frame of the page, and the report has the violations axe-core found attached, in full. `the "css=form" element has no accessibility violations` audits one part of the page, by its name or a selector.

Audit the page in the state the scenario is about: after the form showed its errors, with the menu open. The pack downloads axe-core 4.13.0 the first time, as the web-core pack downloads its browsers.

## Check what a screen reader reads

`the page's accessible structure is:` checks what assistive technology reads on the page, in Playwright's [ARIA snapshot](https://playwright.dev/docs/aria-snapshots) format: a role and a name a line, nested for what is inside:

```gherkin
Scenario: The registration form says what each field is
  When the "/parcels/new" page is opened
  Then the "css=form" element's accessible structure is:
    """
    - paragraph:
      - textbox "Shop account"
    - paragraph:
      - textbox "Parcel reference"
    - group "Service":
      - radio "Standard" [checked]
      - radio "Express"
    - group "Delivery":
      - paragraph:
        - checkbox "Leave with a neighbour" [checked]
    - button "Register the parcel" [disabled]
    """
```

Only what is listed is checked, in its order, as it is nested on the page: here each field is in a paragraph, and the options and the checkbox in their groups. The rest may be anything. A name between slashes is a regular expression, `- paragraph: "/Price: \\d+\\.\\d+ EUR/"` (in quotes, because of the `: ` inside). The check waits for the page to match, 10 seconds or `within {duration}`; when it doesn't, the step says what the page reads instead. The Inspector of a [paused scenario](/guides/watch-web-browsers/#pause-a-scenario) records the structure of what you pick, ready to copy.

## Settings

| Setting | |
| --- | --- |
| `standard` | the WCAG level pages are audited against: `wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa` (the default) or `wcag22aa`; a level includes those below it |
| `ignore` | rules the audits leave out, by their axe-core id, like `color-contrast` |

axe-core is [MPL-2.0](https://github.com/dequelabs/axe-core/blob/develop/LICENSE), by Deque Systems: Axx downloads it for you, and does not ship it.
