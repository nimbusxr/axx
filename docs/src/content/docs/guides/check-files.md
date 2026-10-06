---
title: Check files
description: Check the files your services write to a folder or to cloud storage, whether they are CSV, JSON, PDF, Word or Excel files, by their exact content, their JSON properties, their text or the rows of their table.
---

Services write files for other systems and for people: an import report that a shop's system collects, a workbook for its staff, a handover note the driver signs. The `files` pack checks the files a service writes to a folder, such as an export directory or a volume it shares with Axx. The storage packs have the same checks for the objects in a bucket or a container.

```gherkin
Scenario: The import report says what became of every line
  Given a seeds/manifest-heron.yaml db seed
  Then within 10s the exports folder has a file named manifests/M-HERON-0501/report.csv
  And the manifests/M-HERON-0501/report.csv file in the exports folder has a row where:
    | line   | ML-HER-0501-3         |
    | status | REJECTED              |
    | reason | unknown service level |
```

Add the pack to the project with `axx pack add files` ([Choose packs](/guides/use-packs/)).

## Register the folder

Register the folder under a name, usually in the `Background`. Its `path` is relative to the directory of `axx.yaml`, or absolute, and it can use `${env:..}` and `${sys:..}`:

```gherkin
Background:
  Given the exports folder with the following properties:
    | path | ../infra/exports |
```

The folder may appear only when the service first writes to it. A service in a container writes to a folder of the host through a bind mount:

```yaml title="infra/compose.yaml"
services:
  exports:
    image: busybox:1.37
    volumes: ['./exports:/exports']
    command: ['sh', '-c', 'rm -rf /exports/* && chmod 777 /exports']
  app:
    build: ../app
    environment:
      PARCELS_EXPORT_DIR: /var/lib/parcels/exports
    volumes: ['./exports:/var/lib/parcels/exports']
    depends_on:
      exports:
        condition: service_completed_successfully
```

A folder keeps what earlier runs wrote to it, so a check could pass on a file from the run before. The `exports` service empties the folder before the service starts, and lets a service that runs as a non-root user write to it. Name the files you check after data unique to the scenario, such as a manifest ID: scenarios run in parallel.

## Check a file

A check names a file by its path in the folder, with `/` between folders, and without spaces. Every check waits for the file to be there and to meet the check: 10 seconds, or the time `within` gives. A `*` in the path stands for any characters but a slash, for a file named by something a check cannot know, like the time it was saved: `"snap-*.json"` names the one file it matches, and a check waits while none or several do.

```gherkin
Then within 10s the exports folder has a file named manifests/M-HERON-0501/report.csv
And the manifests/M-HERON-0501/report.csv file in the exports folder is identical to the reports/M-HERON-0501.csv file
And the manifests/M-HERON-0501/summary.json file in the exports folder has the following properties:
  | manifest | M-HERON-0501 |
  | imported | 2            |
  | rejected | 1            |
And the manifests/M-WREN-0503/handover.pdf file in the exports folder contains "Parcels to collect: 2"
```

- `is identical to` compares the file byte for byte with a file of the project, found through `resources`.
- `has the following properties:` reads JSON, like the other JSON property steps: a path and the value as text, `null` for null and `undefined` for absent.
- `contains` reads the file's text, whatever its type (below).

## The text of PDF, Word and Excel files

`contains` reads a file by its type, which its extension tells (or else its content):

| File | Its text |
| --- | --- |
| `.pdf` | the text of its pages, line by line, as the pages show it |
| `.docx` | its paragraphs, its tables (a line per row), its headers and footers |
| `.xlsx` | the cells of every sheet, a line per row, as the workbook shows them |
| `.xml`, `.html` | the text between the tags; the markup counts too |
| `.csv`, `.json`, `.txt` and other text | the text itself |

Case matters. Runs of spaces and line breaks count as one space, so a check does not depend on how a PDF spaces its columns:

```gherkin
Then the manifests/M-WREN-0503/handover.pdf file in the exports folder contains "PX-WRN-5201 Marie Curie, 01067 Dresden 1200 g STANDARD"
```

A PDF of scanned images has no text to read, and neither has a PDF that needs a password to open. Older `.doc` and `.xls` files are not read.

## Rows of CSV files and workbooks

`has a row where:` reads the file as a table whose first row names the columns: a CSV file (with commas or semicolons), a TSV file, or an Excel workbook (its first sheet). One row must have every value of the step's `| column | value |` table:

```gherkin
Then within 10s the manifests/M-OSPREY-0502/report.xlsx file in the exports folder has a row where:
  | line      | ML-OSP-0502-2 |
  | reference | PX-OSP-5102   |
  | status    | IMPORTED      |
```

Cells compare as text, as the workbook shows them: a price formatted with two decimals is `6.90`, not `6.9`. An empty value matches an empty cell.

## Images, compared with their screenshots

`looks like the … screenshot` compares an image a service or an app saves (PNG or JPEG) with its screenshot, pixel by pixel, anti-aliasing aside:

```gherkin
Then within 10s the manifests/M-PLOVER-0504/labels/PX-PLV-5301.png file in the exports folder looks like the "label-PX-PLV-5301" screenshot
```

The first time, there is no screenshot: the step takes it and fails, so that you look at it and keep it (`screenshots/label-PX-PLV-5301.linux.png`). Each platform has its own, named after it, since apps draw their images their own way on each. When the image differs, the step attaches the screenshot, the image and their difference. `packs.files.screenshots` in `axx.yaml` sets their `folder` (default `screenshots`), the share of pixels that may differ (`tolerance`), whether to take them all again (`update`), and the `platforms` a project keeps them for.

## When a check fails

A failed check says what the file holds instead: the lines of its text nearest the text you expected, the rows of its table, or the columns it has.

```text
The manifests/M-HERON-0501/report.csv file in the exports folder did not meet the expectation within 10s: no row has line=ML-HER-0501-3, status=IMPORTED. Its 3 rows are:
  line=ML-HER-0501-1, status=IMPORTED
  line=ML-HER-0501-2, status=IMPORTED
  line=ML-HER-0501-3, status=REJECTED
```

## Objects in cloud storage

The storage packs, `aws-s3`, `gcp-storage` and `azure-blob`, have the same checks for their objects ([Test cloud services](/guides/test-cloud-services/)):

```gherkin
Then within 30s the invoices/kestrel-2026-09.pdf object in the carrier-invoices gcs bucket contains "Total due: 1284.50 EUR"
And the disputes/kestrel-2026-09.csv object in the carrier-invoices gcs bucket has a row where:
  | parcel | PX-KES-1001    |
  | reason | weight differs |
```
