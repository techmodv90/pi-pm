# Requirements — Artifact Markdown File Storage

Source: `.apm/specs/artifacts/ArtifactMarkdownFile.feature` (@draft)
Inferred from requirement: "Store artifact as markdown file"

## From owner / planning

- [x] Resolve NC-1 — artifact scope → all planning stages of `work_item_artifacts`; execution reports out of scope (recorded under the P1 save scenario in the `.feature`)
- [x] Resolve NC-2 — failure semantics → best-effort mirror: canonical save kept, failed projection recorded as warning event, recoverable via backfill (P1 sad path rewritten to `Best-effort projection keeps the canonical save on file-write failure`)
- [x] Resolve NC-3 — write timing → project on every save (recorded under the P1 save scenario)
- [x] Resolve NC-4 — path convention → `<project>/.apm/artifacts/<work_item>/<stage>-r<revision>.md`; existing `.pi/artifacts/plans/` Blueprint render untouched (all scenario paths and the DBML `file_path` comment updated)
- [x] Resolve NC-5 — surface → projection inside artifact-save, not a `pic markdown --out` extension (recorded under the P1 save scenario)
- [x] Resolve NC-6 — `Type:` header → owner confirmed `Type: COMMAND`; header added to the `.feature` file top
- [x] Resolve NC-7 — drift-check trigger → on-demand hash comparison of file bytes against `content_sha256`; no scheduler or verify-flow integration (interpretation recorded under the drift scenario, owner to review)
- [ ] Confirm success criteria are measurable and acceptable (Reliability line amended for NC-2 best-effort semantics — needs owner review)
- [x] Owner approves spec → upgrade tag @draft to @ready (advance gate satisfied: zero open NC markers, zero Auto-QA FAILs; final sign-off requested before any Work Item handoff)

## Traceability (to fill in later)

- [ ] RRI requirement keys:
- [ ] Blueprint verification seam:
- [ ] Contract obligations:
- [ ] Task Graph node(s):
