# Work Item Workflow

The canonical model is a typed Work Item tree. `epic` and `feature` are aggregates; a Feature normally represents a complete vertical slice, while an Epic may contain multiple Features or represent one coherent vertical slice when its approved scope warrants that shape. `task`, `bug`, `chore`, and `gate` are executable leaves. Tasks are bite-sized requirement-bound increments, bound to at most two Requirements, while Code Review checks each child change and aggregate verification is final integrated QA. Archived Epic, Task, and Task Item rows remain migration history and are read-only.

## Lifecycle

```text
Work Item -> Scan -> RRI -> Vision -> Blueprint -> Contracts -> task graph
          -> owner approvals -> materialize -> implementation authorization
          -> Feature or Epic vertical slice -> bite-sized requirement-bound Tasks
          -> Worker -> focused Child Evidence -> Code Review -> Completion Report
          -> aggregate verification (final QA) -> owner acceptance
```

Each staged artifact is an immutable revision with a content hash. Owner checkpoints bind the exact artifact ID, revision, and hash. Publishing a new upstream revision invalidates only downstream checkpoints. Materialization is atomic and creates inactive immutable Task Instruction Packs (TIPs); explicit implementation authorization activates them.

## CLI

```bash
pic work-item create epic "Dependency graph"
pic work-item create feature "Persist graph" --parent <epic-id>
pic work-item create task "Add schema" --parent <feature-id>
pic work-item list
pic work-item show <id>
pic work-item ready
pic work-item status <id> in_progress
```

Artifact and execution commands:

```bash
pic work-item artifact-save <id> scan '<content>'
pic work-item artifact-approve <id> scan <artifact-id> accepted
pic work-item workflow-status <id>
pic work-item graph-validate <id>
pic work-item materialize <id>
pic work-item authorize <id> owner
pic work-item claim <leaf-id> <claimant>
pic work-item aggregate-verify <aggregate-id> passed '<summary>'
pic work-item aggregate-close <aggregate-id>
```

Readiness is derived from status, deferral, claim state, blocking dependencies, and Work Item gates. It is never stored as mutable state. Aggregates cannot be claimed by workers.

## Labels

Labels provide cross-cutting metadata without replacing structured type, status, or priority fields. They are lowercase strings such as `backend`, `auth`, or `release-v1`; children inherit parent labels when created.

```bash
pic work-item create task "Add endpoint" --labels backend,api
pic work-item label add <id> needs-review
pic work-item label remove <id> needs-review
pic work-item label list <id>
pic work-item label list-all
pic work-item list --label backend,api
pic work-item list --label-any frontend,backend
```

`--label` requires every supplied label; `--label-any` requires at least one. Adding or removing an existing or missing label is idempotent.

## Migration

Migration preserves legacy IDs, artifact bytes, hashes, timestamps, reports, and decisions. Former executable parents become Features, archived Task Items remain readable history, and incompatible active pipelines are quarantined rather than restarted. Production releases expose `pic work-item` as the only lifecycle mutation authority; legacy mutation commands and HTTP endpoints fail without changing storage.

## Dashboard

The dashboard lists all Work Item types and shows derived readiness, recursive hierarchy, blockers, gates, artifact revisions, checkpoints, TIP authorization, completion evidence, and verification evidence. It exposes no Task Item controls and routes every type through `/work-item/<id>`.