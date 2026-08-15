# RRI Universal Question Bank — Vibecode Kit v6.0

## How to Use

Select and adapt questions by persona based on Scan evidence, project context, and risk. Auto-answer known facts, skip irrelevant dimensions with a reason, and prioritize P0 critical gaps → P1 business rules → P2 workflow → P3 polish. There is no question quota: stop when unresolved owner-impacting P0/P1 gaps are closed and requirements are testable.

## End User Persona

### Identity & Context
- Who are the primary users, and how many distinct user types exist?
- What is their technical proficiency?
- Where do they use this: office, mobile, field, or elsewhere?
- Is usage daily, weekly, ad hoc, or seasonal?

### Workflow & Goals
- What is the user's main goal when opening this?
- What is the typical end-to-end workflow?
- What happens before and after using it?
- What is the most critical action?

### Pain Points & Expectations
- What frustrates users about the current solution?
- What would make them abandon this tool?
- What would delight them beyond basic functionality?
- Must responses be instant, can users wait, or is batch processing acceptable?

### Access & Devices
- Is the primary device desktop, tablet, mobile, or mixed?
- Is offline access required?
- Are screen reader, keyboard-only, color-blind, or other accessibility requirements present?
- What language or localization support is required?

## Business Analyst Persona

### Goals & Metrics
- What is the primary business goal?
- How is success measured: KPIs, metrics, or conversion targets?
- What is the impact of failure or delay?
- Is the model direct sales, subscriptions, leads, or internal efficiency?

### Rules & Logic
- What are the three to five highest-priority features?
- What approval workflows, calculations, or conditional rules apply?
- What entities exist and how do they relate?
- What reporting or analytics are required?

### Compliance & Constraints
- Do GDPR, HIPAA, SOC2, or industry rules apply?
- Is an audit trail required?
- What data retention policy applies?
- What budget or timeline constrains scope?

### Stakeholders & Process
- Who approves or reviews outputs?
- How does this integrate with existing business processes?
- What notifications or alerts are required?
- Which import or export formats are required?

## QA / Tester Persona

### Input Validation & Boundaries
- What input ranges and formats are valid?
- What should empty states do?
- What maximum data volumes must be handled?
- How many concurrent users are expected?

### Error Handling
- How should errors be communicated?
- In which language should error messages appear?
- Are retry, undo, or rollback required?
- What happens when dependencies fail?

### Security & Data
- Is data public, internal, PII, financial, or health-related?
- Is authentication email/password, SSO, OAuth, API key, or something else?
- What roles, permissions, or row-level authorization apply?
- What input sanitization risks exist?

### Quality Gates
- What response-time, throughput, memory, or UI load-time targets apply?
- What uptime or availability is required?
- Which browsers, devices, operating systems, runtimes, or cloud providers are supported?
- Are unit, integration, end-to-end, or contract tests expected?

## Developer Persona

Use only with an existing codebase.

### Architecture & Patterns
- Which existing patterns should be reused?
- What technical debt must be addressed first?
- What performance bottlenecks exist?
- Are dependencies outdated or vulnerable?

### Code Quality
- What type-safety requirements apply?
- What linting and formatting standards apply?
- What test coverage is expected?
- What inline, API, or README documentation is required?

### Integration
- Which external APIs or services are involved?
- How is authentication with external services handled?
- What synchronization strategy is required?
- Are webhooks or events required?

### Developer Experience
- How much local setup complexity is acceptable?
- What CI/CD is required?
- Are feature flags or staged rollout required?
- What logging or debugging tools are needed?

## Operator Persona

Optional; use when production operations are in scope.

### Deployment & Infrastructure
- Is deployment cloud, self-hosted, or hybrid, and which provider applies?
- Are Docker or Kubernetes required?
- Are development, staging, and production environments required?
- Is deployment blue-green, canary, rolling, or another strategy?

### Monitoring & Observability
- What uptime, error, and performance monitoring is required?
- What alert thresholds and channels apply?
- Is log aggregation required?
- Is tracing or APM required?

### Backup & Recovery
- What backup strategy and frequency apply?
- What RPO and RTO are required?
- Is a disaster recovery plan required?
- What migration and rollback strategy applies?

### Scaling
- What growth is expected?
- Is auto-scaling required?
- What cost constraints apply?
- Are CDN, multi-region, or geographic distribution required?

## Question Mode Guide

| Mode | When to Use | Example |
|------|-------------|---------|
| **CHALLENGE** | Known patterns or evidence supports a likely choice | “I propose JWT auth with refresh tokens. OK?” |
| **GUIDED** | Multiple bounded, domain-specific choices remain | “For export: CSV only, or also PDF and Excel?” |
| **EXPLORE** | The workflow is unknown or genuinely complex | “Describe what happens when an order is disputed.” |

For interactive questions, prefer CHALLENGE when evidence supports a proposal, GUIDED when choices are bounded, and EXPLORE only when necessary. Before asking, apply the Owner-Question Eligibility Gate from `rri.md`: questions must concern observable owner impact, not implementation details. Never include a non-compliant option simply to manufacture a choice.
