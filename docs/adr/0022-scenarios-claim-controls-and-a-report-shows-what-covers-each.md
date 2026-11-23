# 0022: Scenarios claim controls, and a report shows what covers each

- Status: Accepted
- Date: 2026-11-22

## Context

A security engineer asked whether axx could make developers do certain things in their
applications. What security standards ask for is, to a large part, behaviour of a running
system (a short password is refused, a session ends, an admin's action is logged), which is
what an acceptance scenario checks. What is missing is the link: which scenario shows which
control, and which of the controls a project must cover are shown at all.

Two ways to build that link were on the table.

- **The security team writes the tests.** A pack of scenarios, written once for any
  application, that every project must pass. It needs steps of its own, or a way to learn
  each application's shape, and it puts the tests with the people who know the application
  least.
- **The application team proves its controls with its own scenarios.** Whoever approves the
  controls says which are required, and reads the evidence: a security team, a compliance team,
  the system's owner, or a customer or a regulator whose requirements the list comes from. This
  is how compliance works on paper too: the owner of a system says how it meets each control,
  and an assessor judges that.

## Decision

The second. An application team claims a control with a tag on the scenario that proves it,
and axx reports, for every control the project is required to cover, what covers it.

- **A claim is a tag,** `@<framework>:<id>` (`@asvs:6.2.1`): no step is added, and no scenario
  is written for the report's sake. A tag of a framework the project has that names no control
  of it is a finding of lint.
- **A framework is data.** axx knows none: a framework is a key, its controls and the lists of
  them a project can require. A file in the project lists the controls of any framework, and a
  pack registers one for many projects. So no framework needs axx's code, and none waits for
  axx.
- **Text has its own terms.** axx carries the text of the frameworks it may: OWASP's ASVS
  (CC BY-SA 4.0, shown with its source) and the IDs and titles of NIST SP 800-53 (public
  domain). For PCI DSS, ISO 27001 and SOC 2, whoever holds the licence writes the list.
  Evidence names controls by ID and has no framework's text.
- **A run writes evidence, and never fails because of a control.** A run of part of a suite
  cannot know what the rest proves, and a run that reported failure with no failed scenario
  would say something axx's exit codes do not (ADR 0003). The gate is a command, `axx controls
  check`, which judges the evidence of one run or of several together, and which a workflow
  the approver owns can run with its own lists and exceptions.
- **A control is covered by more than scenarios.** A pen test, a scanner or a review covers
  controls, and some do not apply. Other sources' findings and a project's exceptions are part
  of the report, each with where it is from, so the report is the whole picture and an auditor
  reads one document. An accepted exception ends on a day.
- **Every scenario that claims a control must pass,** and a list can say that a control takes
  more than one. Whether a control's scenarios are enough is a person's judgement, and the
  report is made for that judgement: it names each scenario.
- **The packs are a family, and others extend it.** `controls-core` is the engine. Frameworks
  (`controls-asvs`, `controls-nist`), report formats (`controls-oscal`) and sources of evidence
  are packs that register with it, axx's own and anyone's.
- **It is experimental** (ADR 0021), so it adds no exit code and no field of the `--json`
  envelope. `axx controls check` fails with exit 1.

The core gained two things a pack could not do, which other packs can use: a pack is handed the
finished run with each scenario's outcome (`core.RunFinisher`; a hook runs before a scenario's
outcome is set), and a pack can check the project's features, as lint does
(`Manifest.FeatureChecks`).

## Consequences

- A project that does not add a controls pack is not affected: no command, no output, no
  check.
- NIST's baselines are not offered as lists to require: most of a baseline is about people,
  premises and process, which no scenario proves. A project lists the controls its scenarios
  are to prove.
- axx signs nothing. `axx controls check` writes what it judged, the same bytes for the same
  evidence, with its SHA-256, and a project signs that with the tools of its CI.
- A claim can be wrong: a scenario tagged with a control it does not prove. axx cannot tell.
  The report shows each control's scenarios in plain language, and a project that needs more
  guards who may change them.
- Not decided: a mapping from one framework to another (a scenario that claims an ASVS
  requirement counting for a NIST control), and editor support beyond lint's findings.
