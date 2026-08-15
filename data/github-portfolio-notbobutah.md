# GitHub history — github.com/notbobutah

Analysis of the public repositories on the `notbobutah` account, compiled
2026-08-15 (UTC) from the GitHub API: repository metadata, language
breakdowns, commit counts, and each repository's own README.

**How to read this.** The repositories are the primary evidence; this document
is a derived summary of them. Every entry names its source, and every claim
here can be checked against the repository it describes. Where a repository has
no description of its own, the summary is drawn from its README and language
breakdown, and says so.

**Scope.** 14 public repositories, 2016–2026. Two are forks of upstream
projects and are marked as such — a fork is evidence of what was studied or
customised, not of authorship. Commit counts are for the default branch and
count all contributors.

---

## Timeline

| Repository | Created | Last push | Primary language | Commits | What it is |
|---|---|---|---|---|---|
| [pav_test](https://github.com/notbobutah/pav_test) | 2016-06-02 | 2016-06-02 | JavaScript | fork | Fork of a Nodeclipse-generated Node scaffold. No original content of note. |
| [SuiteCRM](https://github.com/notbobutah/SuiteCRM) | 2016-07-13 | 2016-07-13 | PHP | fork | Fork of SuiteCRM 7.6.5, the open-source enterprise CRM (~33 MB of PHP). Upstream code, not authored here. |
| [angular-swagger-sequelize](https://github.com/notbobutah/angular-swagger-sequelize) | 2017-07-17 | 2017-07-17 | TypeScript | 2 | Full-stack card demo wiring Angular CLI to a Swagger-defined API over Sequelize/Postgres. An integration spike across four generators. |
| [fix-datapump](https://github.com/notbobutah/fix-datapump) | 2018-04-05 | 2018-04-05 | Java | 1 | FIX engine built on QuickFIX/J to generate load against an FX data server, speaking FIX 4.4 against the FXSpotStream ROE. |
| [fix-datapump-client](https://github.com/notbobutah/fix-datapump-client) | 2018-04-05 | 2018-04-05 | Java | 1 | The client half: an HTTP/2 streaming service for FX symbols that issues FSS-specific FIX requests to load-test the streaming path. Spring/SpringFox + QuickFIX/J. |
| [google-maps-react-demo](https://github.com/notbobutah/google-maps-react-demo) | 2018-06-13 | 2018-06-13 | JavaScript | 2 | React weather map: a movable marker driving a floating info box of weather data. Small, single-purpose demo. |
| [WhiteLine](https://github.com/notbobutah/WhiteLine) | 2018-09-08 | 2018-10-07 | Python | 2 | Customisations on the SunFounder PiCar-S robotics SDK — line-following on a sensor-equipped Raspberry Pi car. Vendor SDK plus modifications. |
| [on-xml-proxy](https://github.com/notbobutah/on-xml-proxy) | 2019-04-15 | 2019-04-15 | Go | 1 | SOAP/XML examples in Go (~10 KB). The only Go on the account before 2026. |
| [DevOps-Terraform-AWS](https://github.com/notbobutah/DevOps-Terraform-AWS) | 2020-02-15 | 2020-02-18 | HCL | 1 | Terraform definitions and setup notes for provisioning an AWS EKS cluster, including CLI/toolchain configuration. |
| [SpotLight](https://github.com/notbobutah/SpotLight) | 2020-02-21 | 2023-03-03 | TypeScript | 39 | Full-stack demo: Angular 7 + SyncFusion front end, Node/Swagger API, with Terraform (HCL) and Docker in the tree. The most sustained of the early projects. |
| [SpotLight-2](https://github.com/notbobutah/SpotLight-2) | 2022-09-06 | 2023-01-07 | JavaScript | 4 | Second iteration of the Spotlight demo, re-based on a newer JS stack. |
| [Spotlight-REST-JPA](https://github.com/notbobutah/Spotlight-REST-JPA) | 2023-01-20 | 2023-01-23 | Java | 18 | The Spotlight API re-implemented in Spring Boot 2.7 on Java 17 — JPA/Hibernate against Postgres, Springfox/Swagger, containerised. |
| [Spotlight-IoT](https://github.com/notbobutah/Spotlight-IoT) | 2023-02-23 | 2023-06-23 | TypeScript | 56 | The largest project on the account. Merges two prior projects into one deployable system: React diagramming front end, Node REST API, the ThingsBoard IoT platform, a Raspberry Pi pseudo-device in a VM, docker-compose for local clusters, and deployment to Google Kubernetes Engine via GitHub Actions. Carries a day-by-day task list and a LucidChart deployment diagram. |
| [GoReactChat](https://github.com/notbobutah/GoReactChat) | 2026-08-15 | 2026-08-15 | Go | — | This project: a gRPC/Connect streaming chat service in Go with a React (Next.js) client, retrieval over local embeddings, and a container image published to GitHub Packages. |

---

## What the history shows

**A pattern of end-to-end delivery, not fragments.** The substantial
repositories are complete systems rather than exercises: Spotlight-IoT (56
commits) spans a React front end, a Node API, a third-party IoT platform, a
simulated hardware device, local orchestration via docker-compose, and cloud
deployment to GKE through GitHub Actions. Its README opens with a requirements
list and carries a day-by-day task breakdown — the work was scoped and tracked,
not improvised.

**One system, re-built four times across three years.** The Spotlight arc
(2020 → 2023) is the clearest signal on the account. The same application is
rebuilt as the stack changes: Angular 7 + Node (SpotLight, 39 commits), a
JavaScript re-base (SpotLight-2), a Java 17 / Spring Boot 2.7 / JPA back end
(Spotlight-REST-JPA, 18 commits), then an IoT and Kubernetes deployment
(Spotlight-IoT, 56 commits). Re-implementing a familiar domain on an unfamiliar
stack is how the stack gets learned properly.

**Polyglot by evidence, not by claim.** Primary languages across the account:
Java, TypeScript, JavaScript, Python, Go, HCL, PHP. Several repositories mix
Terraform, Dockerfiles and shell alongside application code, so infrastructure
travelled with the application rather than being someone else's problem.

**Financial-systems work is represented.** The two 2018 FIX repositories
implement a QuickFIX/J-based FIX 4.4 engine and an HTTP/2 streaming client for
FX market data, built specifically to load-test a streaming data service. That
is corroborating evidence for the real-time trading and fintech experience
claimed on the résumé.

**Deployment and infrastructure recur.** Terraform appears in
DevOps-Terraform-AWS (EKS), inside SpotLight, and again in Spotlight-IoT (GKE),
across AWS and GCP.

---

## Gaps, stated plainly

**Public Go is thin before this project.** Excluding GoReactChat, the account
contains one Go repository: `on-xml-proxy`, about 10 KB of SOAP/XML examples
with a single commit, from 2019. For a role asking for strong hands-on Go, the
public history alone does not carry that claim — it rests on the professional
work described in the résumé plus this project. Anyone assessing Go depth from
GitHub should read GoReactChat itself, which is the substantive Go artifact.

**Commit counts are low on the older repositories.** Several were pushed as a
single commit, so the history shows the finished artifact rather than the
process that produced it. Repository size and README depth are better signals
than commit count on those.

**The account is not a complete record of the work.** The most substantial
platform described on the résumé — a 20-service, 30-repository AI-native
marketing platform — is not public, and would not be. This account shows
personal and demonstration projects; it is a sample of range, not a portfolio
of the professional work.

**Two entries are forks.** SuiteCRM and pav_test are upstream code. They belong
in a history of what was studied, not in a list of what was built.

**Recency.** Between mid-2023 and this project the account is quiet. That is a
gap in the public record, not necessarily in the work.

---

## Provenance

Compiled from the GitHub REST API on 2026-08-15 (UTC):

- `GET /users/notbobutah/repos` — names, descriptions, creation and push dates,
  fork and archive flags, primary language
- `GET /repos/{owner}/{repo}/languages` — byte-level language breakdown
- `GET /repos/{owner}/{repo}/contributors` — commit counts (default branch, all
  contributors)
- `GET /repos/{owner}/{repo}/readme` — each repository's own description of
  itself

Descriptions above are summaries of that material. Where a repository set its
own description, that wording informed the summary; where it did not, the
summary comes from the README and language mix. Nothing here is inferred from
outside these sources.

Regenerate by re-running those queries — the account changes, and this document
is a snapshot with a date on it, not a live view.
