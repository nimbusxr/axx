Every step of Axx's packs follows the same grammar. The pack pages list each step with its variants, parameters and an example.

## Optional parts

An expression writes optional parts as `[[ ... ]]`. A step matches with or without each of them. `the[[ {ordinal} ordered]] response status code is {int}[[ on {service}]]` matches:

- `the response status code is 200`
- `the 2nd ordered response status code is 201`
- `the response status code is 200 on parcels`
- `the 2nd ordered response status code is 201 on parcels`

`axx steps show <id>` prints every variant of a step.

## Services

A step without a service name uses the default service: the first one of its kind registered in the scenario. The named form (`on parcels`, `on parcels-db`, `on the events kafka service`) picks another.

## Ordinals

`1st`, `2nd`, `3rd`, `4th`, ... count requests, selections and events within a scenario, starting at 1. A step without an ordinal uses the first.

## Arguments

- `{string}` takes single or double quotes; the quotes are removed.
- `a(n)`, `time(s)`, `row(s)` and `document(s)` are optional text: `1 time` and `2 times` both match.
- A step that ends with `:` takes a two-column data table on the following lines.
- How a value is typed (string, number, `null`, `undefined`) depends on the step; each step's entry says how it reads its values.

## Parameter types

