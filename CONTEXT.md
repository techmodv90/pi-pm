# Work Item Planning Context

This context defines the language used to design, decompose, review, and verify Work Items.

## Work Item Model

**Epic**:
A broad delivery outcome that may contain Features and owns an aggregate acceptance boundary.
_Avoid_: one giant Task, project bucket

**Feature**:
A coherent, demonstrable vertical slice of behavior that can be accepted as an aggregate.
_Avoid_: frontend phase, backend phase, one Task

**Vertical Slice**:
A complete user-visible or system-observable capability that crosses the technical boundaries required by its behavior.
_Avoid_: horizontal layer, architecture slice

**Task**:
A bite-sized, independently verifiable implementation increment inside a Feature or Epic.
_Avoid_: all backend, all frontend, implementation bucket

## Evidence And Decisions

**Requirement**:
An authoritative behavioral obligation expressed with Given/When/Then acceptance criteria.
_Avoid_: wish, ticket detail

**Child Evidence**:
Focused tests and verification output showing how one Task contributes to its bound Requirements.
_Avoid_: final QA, aggregate approval

**Code Review**:
A child-level review of a candidate change against the Task contract and repository standards.
_Avoid_: QA gate, product acceptance

**Aggregate Verification**:
The final integrated QA evaluation of a Feature or Epic against its complete Requirements and cross-slice behavior.
_Avoid_: child review, spot check

**Owner Acceptance**:
The explicit owner decision that follows current passed Aggregate Verification.
_Avoid_: reviewer approval, automatic closure

## Codebase Design

**Module**:
Anything with an Interface and an Implementation, from a function or package to a tier-spanning vertical slice.
_Avoid_: component, service, unit

**Interface**:
Everything a caller must know to use a Module correctly, including invariants, ordering, errors, configuration, and performance.
_Avoid_: API, signature

**Implementation**:
The behavior hidden behind a Module's Interface.
_Avoid_: Adapter when the concern is simply internal behavior

**Seam**:
The location where a Module's Interface allows behavior to change without editing the caller.
_Avoid_: boundary when referring to module placement

**Adapter**:
A concrete implementation that satisfies an Interface at a Seam.
_Avoid_: abstraction created only for hypothetical variation

**Depth**:
The leverage provided by a small Interface that hides substantial behavior.
_Avoid_: implementation-to-interface line ratio

**Leverage**:
Capability callers gain per unit of Interface they must learn.

**Locality**:
The concentration of change, bugs, knowledge, and verification behind one Interface.
