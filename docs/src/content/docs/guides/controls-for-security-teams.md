---
title: Controls for security teams and auditors
description: How a security team sets the controls its application teams must cover, keeps the list, the exceptions and the gate out of a project's own hands, and what an auditor is given and can verify.
---

[Prove security controls](/guides/prove-controls/) is written for the team that builds an application. This page is for the people on the other side: the security team that decides what must be covered, and the auditor who reads the result.

:::caution[Experimental]
The controls packs are [experimental](/explanations/stability/): they may change in any release.
:::

## Who does what

| | Decides | Does |
| --- | --- | --- |
| Security team | which controls are required, and which exceptions are approved | keeps the list and the exceptions, and owns the gate |
| Application team | how its application meets each control | writes the scenarios, tags them, and records what is covered elsewhere |
| Auditor | whether the evidence is enough | reads the report, samples the scenarios, and verifies where the report came from |

The security team writes no tests. A team that knows the application proves each control with a scenario in plain language, and the security team reads that scenario.

## Agree on the list

1. Start from the framework you work to. `axx controls list` shows the lists the packs carry, such as the three levels of OWASP ASVS. For NIST SP 800-53, PCI DSS, ISO 27001 or your own rules, write the list as a file ([A framework of your own](/guides/prove-controls/#a-framework-of-your-own)).
2. Go through it with the application team, one control at a time. `axx controls show asvs:8.2.2` prints what a control requires. Each control ends up in one of three places:
   - **A scenario will prove it.** It stays in the list.
   - **Something else covers it:** a pen test, a scanner, a review. It stays in the list, and an [evidence file](/guides/prove-controls/#what-a-scenario-cannot-prove) names the source.
   - **It does not apply,** or the risk is accepted for a time. It stays in the list, with an exception that says why.
3. Say how many scenarios a control takes, where one is not enough: `scenarios: 2` for a control with two sides.

A control never leaves the list because it is hard. It stays, and the report shows it as not covered until something covers it.

## Keep the list, the exceptions and the gate

A project's settings are files a developer, or a coding agent, can change. Three things must not be theirs alone to change: the list, the exceptions, and the check itself.

**The simple way: files in the project, which need your review.**

1. Keep the list and the approved exceptions in a folder of the project, and name both in the check:

   ```sh
   axx controls check --require security/controls.yaml --exceptions security/exceptions.yaml
   ```

   ```yaml
   # security/exceptions.yaml
   exceptions:
     - control: asvs:5.2.1
       kind: not-applicable
       reason: The service takes no files.
     - control: asvs:6.3.3
       kind: accepted
       reason: Multi-factor sign-in ships in the June release.
       until: 2027-06-30
   ```

2. Make that folder, and the CI file that runs the check, need the security team's review. On GitHub, that is a `CODEOWNERS` file and a branch rule that requires the owners' review:

   ```text
   /security/                  @your-org/security
   /.github/workflows/ci.yml   @your-org/security
   ```

A team can still write an exception into its own `axx.yaml`. With `--exceptions`, that is a request: the report lists it under "Exceptions the project asks for, which are not counted", and the control stays not covered until the security team adds it to the approved file.

**The stronger way: nothing in the project.**

1. Publish the list as a [controls pack](/guides/write-a-controls-pack/), or keep it in a repository of your own. Its version is the baseline's version.
2. Run the check in a workflow the security team owns and every repository must pass (on GitHub, a ruleset that requires a workflow). That workflow gets the lists and the approved exceptions from your repository, and the evidence from the project's runs.

Either way, a run never fails because of a control. Only the check does, so a team is not blocked while it works toward the list, and the gate is switched on when you agree it is time.

## Review what stands behind a control

A tag is a claim. `@asvs:8.2.2` on a scenario says "this proves it", and Axx cannot tell whether that is true. A person can, quickly:

- **In a pull request.** `axx controls report --format markdown` lists each control with its scenarios by name. Written to the CI job's summary, it shows a reviewer what stands behind every control, and what a change took away.
- **By reading the scenario.** A scenario is a few lines of plain language: what the system starts from, what someone does, and what must be true afterwards.
- **By requiring your review of the features that matter most.** A `CODEOWNERS` line for the folder of the sign-in features, say.

What Axx does check: a tag that names no control is reported, a control whose scenario fails is failing whatever else covers it, and a tag that is removed leaves its control not covered, which the gate refuses.

## Exceptions that end

An accepted exception has a last day. From the day after, its control counts as not covered and the gate fails, so a risk is never accepted by forgetting it. Every exception, with its reason, is in every report.

## For auditors

Ask for these, of the commit that was released:

1. **The report,** in the format you work with: Markdown to read, CSV for a spreadsheet, or OSCAL assessment results (`controls-oscal`) for a tool that reads them.
2. **The file the check wrote,** `checked.json`, and its attestation, where the project signs it ([Sign what was checked](/guides/prove-controls/#sign-what-was-checked)).
3. **The evidence files** of other sources, each with a link to its report.

What each entry tells you:

| In the report | What it means | What to look at |
| --- | --- | --- |
| proven | every scenario tagged with the control passed in that run | the scenarios: each has its file and line at that commit |
| covered | another source says so | the source's own report, by its link |
| excepted | the control does not apply, or its risk is accepted until a day | the reason; who approved it is in the history of the exceptions file |
| partly proven, failing, not covered | the control is not met | |

To verify where a signed file came from, on GitHub:

```sh
gh attestation verify checked.json --repo your-org/your-repo
```

It confirms that the file was made by that repository's workflow, at the commit it names, and has not changed since.

**What the report does not say.** That a control is proven means its scenarios passed, against the running system, in that run. Whether those scenarios are enough for the control is your judgement: the report gives you each of them to read. It is evidence, and no certification.
