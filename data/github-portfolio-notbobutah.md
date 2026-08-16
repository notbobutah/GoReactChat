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
| [GoReactChat](https://github.com/notbobutah/GoReactChat) | 2026-08-15 | 2026-08-16 | Go | 25 | This project: a gRPC/Connect streaming chat service in Go with a React (Next.js) client, retrieval over local embeddings, and two agents — a hand-written streaming tool-use loop against the Anthropic Go SDK, and a research agent whose tool loop executes server-side on xAI. Deployed to Kubernetes behind TLS; images published to GitHub Packages. |

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

## What this account does and does not cover

**Two agent implementations, deliberately different.** GoReactChat contains
both shapes of agent, which is the more useful signal than either alone. The
chat runs a tool-use loop written by hand in Go against the Anthropic SDK —
accumulating tool calls mid-stream, dispatching them, feeding results back, and
discarding a "let me check" preamble once a retrieval tool returns. The news
watcher does the opposite: a single request to xAI's Responses API declares the
tools, and the model runs roughly fifteen web searches, reads and iterates on
xAI's infrastructure. No loop, no scheduler and no extra service on this side.

The second one is the more interesting engineering problem, because moving the
loop off your own machine moves the risk rather than removing it. A scan takes
about a minute, so the result cannot be returned in a response — it arrives over
a server-streaming RPC the browser subscribes to. And it bills per tool call
rather than per token, so the usual token budget does not bound it: spend is
capped by frequency instead — it runs only while somebody is subscribed, once
per interval, one scan at a time, with the last result persisted so a restart
restores instead of rescanning. The digest reports its own tool-call count in
the UI, on the principle that an autonomous agent whose cost is invisible is one
nobody notices running away.

**Kubernetes evidence is in the repository, not just on the résumé.** The
Kubernetes and cloud experience described under the professional roles is
commercial and therefore private — but GoReactChat is not, and it runs on
Kubernetes. Its `deploy/` directory is committed and readable: Deployment and
Service manifests for both processes, an nginx Ingress splitting one host
across them by path, a cert-manager Certificate, a ConfigMap, and a Secret
template that documents the required keys while deliberately containing none of
their values, because the repository is public.

The manifests are worth opening rather than counting. Both pods run
`runAsNonRoot` with a read-only root filesystem, all capabilities dropped and
`seccompProfile: RuntimeDefault`; both set resource requests and limits; both
carry readiness, liveness and startup probes, the startup probe sized for a boot
that loads a document corpus. The API deployment is pinned to a single replica
with `strategy: Recreate` and the manifest says why — the rate limiter and the
in-memory half of the token budget are per-process, so a second pod would
silently double the global cap, and a rolling update would do the same for the
length of the rollout. The ingress carries `proxy-buffering: "off"`, without
which nginx holds a streaming response until the handler finishes and the stream
quietly stops being a stream. `.github/workflows/deploy-dev.yml` applies the
manifests, restarts the rollouts and then smoke-tests the service through the
ingress, on the reasoning that a ready pod proves a process started rather than
that the deployment is reachable.

That is the difference between claiming Kubernetes and showing it: the decisions
are legible, the trade-offs are written down next to the setting they explain,
and a reader can check every one of them against a running deployment.

**Public Go begins with this project.** Before GoReactChat the account has one
Go repository — `on-xml-proxy`, about 10 KB of SOAP/XML examples from 2019.
Go experience prior to that is professional rather than public: the résumé
records gRPC services in Go and Java for high-performance inter-cluster
communication. For a current read on Go depth, GoReactChat is the artifact to
read — it is a complete service, and the code is public.

**Commit counts reflect how the work was published, not how it was done.**
Several of the older repositories were pushed as a single commit, so they show
the finished artifact rather than the history that produced it. On those,
repository size and README depth are the better signal.

GoReactChat is the exception, and deliberately so: its history is incremental
and readable, each commit scoped to one change with the reasoning in the message
rather than in someone's memory. It is the one repository on the account where
how the work was done is visible, not just what it produced — which is the point
of shipping it publicly for this application.

**This is a sample, not the body of work.** The platform described on the
résumé — 20 services across 30 repositories, built and operated with a
three-engineer team — is private, as commercial work is. This account holds
personal and demonstration projects; it shows range and independent build
history, not the professional estate.

**Two entries are forks.** SuiteCRM and pav_test are upstream code, listed for
completeness and marked as such rather than presented as authored work.

## Provenance

Compiled from the GitHub REST API on 2026-08-16 (UTC):

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
