# Expona AI — engineering estate (aggregate summary)

A statistical summary of the private GitHub organisation behind the AI-native
marketing-intelligence platform described on the résumé, compiled 2026-08-15
(UTC) from the GitHub API.

**Deliberately aggregate.** The organisation is private and includes work built
for clients. Repository names, client names, service topology, and per-project
descriptions are withheld: this document reports counts, language mix, cadence
and themes only. It exists so the scale and shape of the work can be discussed
in a public context without publishing a private company's internal map. The
underlying repositories cannot be linked or read here; they are available to
review under NDA in an interview setting.

**Why it is a static file.** The organisation is not reachable at runtime — it
is private, and this application holds no credentials for it. The figures below
are a snapshot with a date on it, not a live view.

---

## Scale

| Measure | Value |
|---|---|
| Repositories | 36 (all private; 0 public) |
| Active period | 2024-10-29 → 2026-08-14 (~21.5 months) |
| Archived | 1 |
| Pushed in the last 30 days | 15 |
| Pushed in the last 90 days | 19 |
| Repositories with a lifespan over 6 months | 5 |
| Median repository lifespan (first to last push) | 25 days |

This is the estate behind the résumé's "20-service, 30-repository AI-native
marketing-intelligence platform … operated end-to-end by a 3-engineer team."
The repository count corroborates that claim directly.

## Language mix (primary language per repository)

| Language | Repositories |
|---|---|
| TypeScript | 20 |
| Python | 9 |
| Shell | 3 |
| JavaScript | 2 |
| HTML | 1 |
| (none detected) | 1 |

A two-language core — TypeScript for the web application, agent runtime, and
integrations; Python for the generation and retrieval services — with shell
carrying the infrastructure and deployment automation.

## Creation cadence

| Quarter | New repositories |
|---|---|
| 2024 Q4 | 1 |
| 2025 Q1 | 1 |
| 2025 Q3 | 7 |
| 2025 Q4 | 8 |
| 2026 Q1 | 6 |
| 2026 Q2 | 7 |
| 2026 Q3 | 6 |

A single application through late 2024, then sustained expansion from mid-2025
at roughly 6–8 new repositories per quarter for five consecutive quarters.

## Themes

Grouped by function, without naming the repositories:

- **Core platform** — the customer-facing web application and its public site.
- **Agent and generation services** — Python services running agent crews that
  produce personas, competitive intelligence, and campaign content.
- **Retrieval** — a vector-database-backed knowledge service providing semantic
  retrieval to those agents, plus the tooling that loads and manages its corpus.
- **Assistant / agent runtime** — the conversational layer, including a
  clean-slate rebuild of the earlier implementation.
- **Infrastructure and delivery** — CI/CD automation, environment provisioning,
  and developer tooling.
- **Third-party integrations** — CRM, marketing-automation and social
  publishing connectors, each in its own repository.
- **Internal tools** — administration, analytics, inventory and survey
  utilities.
- **Client and product sites** — separate deliverables built on the platform's
  foundations.

## What the shape indicates

**Sustained delivery, not a burst.** Nineteen of 36 repositories were pushed to
within the last 90 days, and five have been maintained for over six months. The
estate is in active use, not archived.

**Service-per-repository discipline.** A median lifespan of 25 days alongside
five long-lived repositories is the signature of a stable core with
short-lived, single-purpose services and integrations built around it — each
integration isolated rather than accumulating inside the main application.

**Breadth carried by a small team.** Thirty-six repositories across seven
functional areas, maintained by a three-engineer team over roughly 21 months,
implies heavy reliance on automation and consistent scaffolding rather than
per-project bespoke setup.

---

## Limits of this summary

- **No verification is possible from outside.** Every figure here comes from a
  private organisation. A reader cannot check them; they are offered as a
  self-reported summary, and should be weighted accordingly.
- **Counts are not contributions.** These are organisation repositories, not a
  personal commit history. They show the estate that was directed and built
  with a small team, not individual authorship of every line.
- **Repository count is not service count.** Some repositories are integrations
  or sites rather than runtime services.
- **Aggregates hide variance.** A median lifespan of 25 days spans repositories
  that ran for two years and repositories that were created and finished in a
  day.

## Provenance

`GET /orgs/Expona-AI` and `GET /orgs/Expona-AI/repos` via the GitHub API on
2026-08-15 (UTC), authenticated as an organisation member. Statistics derived
from repository metadata only: creation date, last push, primary language, and
archive status. No repository contents were read for this summary, and no
names, descriptions, or client identifiers are reproduced in it.
