---
title: Prove security controls
description: Show which of the controls your project is required to cover its scenarios prove - OWASP ASVS, NIST SP 800-53, PCI DSS or your own rules - with one report for security teams and auditors, and a check that fails when a required control is not covered.
---

A security standard is a list of controls: "passwords are at least 8 characters long", "a session ends when the user signs out". Many of them are statements about how a running system behaves, which is what a scenario checks.

With the controls packs, a scenario says which control it proves, and Axx keeps count: which of the controls your project is required to cover are proven, by which scenarios, and which are not.

```text
controls: 41 of 52 required are covered (38 proven, 3 by other evidence), 3 excepted, 8 not covered
  asvs:6.3.1  not covered    no scenario is tagged @asvs:6.3.1
  asvs:7.4.1  failing        features/sign-out/sign-out.feature:12 failed
```

:::caution[Experimental]
The controls packs are [experimental](/explanations/stability/): they may change in any release. Nothing about them affects a project that does not add them.
:::

## Set it up

1. Add the pack of the framework you work to. It brings `controls-core`, which does the counting.

   ```sh
   axx pack add controls-asvs     # OWASP ASVS 5.0
   axx pack add controls-nist     # NIST SP 800-53, revision 5
   ```

   For any other framework, add `controls-core` alone and list the controls yourself ([below](#a-framework-of-your-own)).

2. Say which controls the project is to cover, in `axx.yaml`:

   ```yaml
   packs:
     controls-core:
       require: [asvs-5.0-level-1]
   ```

   `axx controls list` shows the lists your packs carry.

3. Tag each scenario with the control it proves. The tag is the framework's key, a colon, and the control's ID:

   ```gherkin
   @asvs:6.2.1
   Scenario: A password under 8 characters is refused
     Given a POST request to /api/accounts
     And a request payload using an application/json empty content template
     And the request payload properties are:
       | email    | hanna@example.com |
       | password | "short"           |
     When the request is executed
     Then the response status code is 422
   ```

   A tag on a `Feature` or a `Rule` covers every scenario under it. A scenario can carry several tags, of several frameworks.

4. Run as usual. The run ends with the summary above, and writes what it proved to `.axx/controls/run.json`. A run never fails because of a control.

## Find the control to tag

Search by a word, then read the one you mean:

```sh
axx controls list password length
axx controls show asvs:6.2.1
```

`show` prints what the control requires, the list that requires it, and how it stands: the scenarios that claim it, and whether they passed.

A tag that names a control its framework does not have is most likely mistyped. `axx lint`, `axx validate` and `axx run` warn about it and suggest the nearest control, and your editor marks the tag.

## How a control stands

| State | Meaning |
| --- | --- |
| proven | every scenario tagged with it passed |
| covered | something other than a scenario covers it |
| excepted | the project recorded an exception |
| partly proven | its scenarios passed, and it takes more of them |
| failing | a scenario tagged with it did not pass, or another source says it fails |
| not covered | nothing covers it |

The first three count as covered. The others do not.

**Every scenario tagged with a control must pass.** One that fails makes the control failing, whatever else covers it.

## What a scenario cannot prove

Some controls are not about behaviour ("the design is documented"), and some are checked by other means. Record both, so the report is complete.

**Evidence from elsewhere.** A pen test, a scanner or a review covers controls too. Write a file that names its source and the controls it covers or fails:

```yaml
# security/pen-test-2027-03.yaml
source: Pen test, March 2027
link: https://example.com/reports/pen-test-2027-03
date: 2027-03-14
controls:
  asvs:7.1.1: covered
  asvs:8.2.2:
    state: failing
    note: Finding 12, open
```

**Exceptions.** A control that does not apply, or a risk that is accepted for a time:

```yaml
packs:
  controls-core:
    require: [asvs-5.0-level-1]
    evidence: [security/pen-test-2027-03.yaml]
    exceptions:
      - control: asvs:5.2.1
        kind: not-applicable
        reason: The service takes no files.
      - control: asvs:6.3.3
        kind: accepted
        reason: Multi-factor sign-in ships in the June release.
        until: 2027-06-30
```

An accepted exception ends with its day. From the day after, the control counts as not covered again. Every exception shows in every report, with its reason.

## The report

```sh
axx controls report                              # in the terminal
axx controls report --format markdown            # for a pull request or a wiki
axx controls report --format csv -o controls.csv # for a spreadsheet, or a tool that takes one
axx controls report --format json                # as data
```

The report lists every required control with what covers it: its scenarios by name and file, the other evidence with its link, and the exception with its reason. Controls that are covered and in no required list come after.

Other packs add formats.

### OSCAL

OSCAL is NIST's format for security assessments, which United States government programs and many compliance tools read. Add the `controls-oscal` pack, and the report is written as OSCAL assessment results:

```sh
axx pack add controls-oscal
axx controls report --format oscal --out assessment-results.json
```

- **Each framework is a result,** with the controls it reviewed.
- **Each scenario is an observation** of a test, with its feature file and line as the evidence. What another source says is an observation too, with its link.
- **Each control is a finding:** satisfied or not satisfied, with the observations it rests on.
- **A control that does not apply** is satisfied, and its implementation status says not-applicable, with the reason.
- **A risk accepted until a day** is a risk whose deviation is approved, with that day as its deadline. Its control stays not satisfied.

The document is valid against NIST's schema (OSCAL 1.1.3), and names controls as OSCAL does: `ia-5.1` for NIST's IA-5(1), `V6.2.1` for an ASVS requirement. It names no assessment plan, as the scenarios have none: a note in the document says so. A program that requires one (FedRAMP's, say) has rules of its own beyond the format, which the document does not claim to meet.

## Turn on the gate

`axx controls check` fails when a required control is not covered. Add it to CI when the project is ready:

```sh
axx run
axx controls check
```

It reads the evidence the run wrote, prints what is not covered, and exits with 1 when something is not.

**When CI splits the suite across jobs,** no single job sees every scenario. Each job keeps its evidence, and one last job checks them together:

```sh
# in each job
axx run --shard 1/3            # writes .axx/controls/run.json

# in the last job, with each job's evidence downloaded
axx controls check shard-1/ shard-2/ shard-3/
```

A control is proven when the scenarios that claim it passed, across all the runs. Runs of different commits are refused.

**For a gate a project cannot lower,** run the check in a workflow the security team owns, and say where the lists and the exceptions come from:

```sh
axx controls check --require security-baseline.yaml --exceptions approved-exceptions.yaml
```

The project's own exceptions are then shown in the report as asked for, and do not count. [Controls for security teams and auditors](/guides/controls-for-security-teams/) has the whole arrangement: who keeps the list, how an exception is approved, and what an auditor is given.

### Sign what was checked

`check` writes what it judged to `.axx/controls/checked.json` and prints the file's SHA-256. The file is the same, byte for byte, for the same evidence. Sign it with the tool your CI already has. On GitHub, an attestation says which workflow made the file, on which commit:

```yaml
permissions:
  id-token: write
  attestations: write

steps:
  - run: axx controls check shard-*/
  - uses: actions/attest-build-provenance@v4
    with:
      subject-path: .axx/controls/checked.json
```

An auditor then checks the file against it:

```sh
gh attestation verify checked.json --repo your-org/your-repo
```

On other CI systems, `cosign sign-blob` signs the same file.

## Several scenarios for one control

A control with several sides needs a scenario for each: a short password is refused, and a long one is accepted. Tag each of them. The report names every scenario behind a control, so a reviewer can judge whether they are enough.

A list of your own can say how many it takes:

```yaml
controls:
  "8.3.4":
    title: An account locks after repeated wrong passwords
    scenarios: 2
```

With fewer passing scenarios than that, the control is partly proven, which does not count as covered.

## A framework of your own

For PCI DSS, ISO 27001, SOC 2 or your own rules, list the controls in a file. Axx ships none of their text: those standards are copyrighted, and whoever holds the licence writes the titles.

```yaml
# security/pci.yaml
framework: pci
title: PCI DSS
version: "4.0.1"
controls:
  "8.3.6": Passwords are at least 12 characters long
  "8.3.4":
    title: An account locks after repeated wrong passwords
    scenarios: 2
```

```yaml
packs:
  controls-core:
    require: [security/pci.yaml]
```

Scenarios then claim its controls with `@pci:8.3.6`. The same file works for a framework a pack carries: `framework: nist` with the IDs of the NIST controls your scenarios are to prove.

To give one list to many repositories, or to add a report format or a way to read another tool's results, [write a controls pack](/guides/write-a-controls-pack/).

## What Axx does not claim

- **"Proven" means that the control's scenarios passed.** It does not mean that those scenarios test everything the control asks for. Axx cannot judge that, and a person can: the report names each control's scenarios, in plain language, so a reviewer can read them and decide.
- **Axx checks behaviour.** It does not read code, so "queries are parameterized" is not something a scenario shows; "this injection attempt is refused" is.
- **A report is not a certification.** It is evidence for the people who decide.
