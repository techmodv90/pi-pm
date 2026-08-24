# Deepening

Use this guide when consolidating a cluster of shallow Modules. It assumes the vocabulary in `SKILL.md`.

## Dependency Categories

### In-Process

Pure computation or in-memory state with no I/O. Merge the Modules and test directly through the new Interface. No Adapter is needed.

### Local-Substitutable

A dependency with a local test stand-in, such as an in-memory filesystem or embedded database. Test the deepened Module with that stand-in. Keep the Seam internal rather than exposing a port through the external Interface.

### Remote But Owned

A service owned by the project across a network Seam. Define a port at the Seam, inject a production transport Adapter, and test the deep Module with an in-memory Adapter.

### True External

A third-party dependency the project does not control. Inject it through a port and use a mock Adapter in tests.

## Seam Discipline

- One Adapter means a hypothetical Seam; two justified Adapters make it real.
- Keep test-only internal Seams private to the Implementation.
- Do not expose internal Seams through the external Interface merely because tests use them.

## Replace, Do Not Layer

- Replace shallow-module tests with tests through the deepened Module's Interface when they cover the same behavior.
- Assert observable outcomes through the Interface, not internal state.
- Tests should survive internal refactors. If they change whenever the Implementation changes, they are testing past the Interface.
