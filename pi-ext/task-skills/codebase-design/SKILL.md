---
name: codebase-design
description: Design deep Modules with small Interfaces, clean Seams, justified Adapters, high leverage, locality, and interface-level tests. Use when introducing or changing module interfaces, deciding seam placement, deepening shallow modules, improving testability, or comparing alternative interfaces.
license: MIT
---

# Codebase Design

Use this vocabulary exactly: **Module**, **Interface**, **Implementation**, **Adapter**, **Depth**, **Seam**, **Leverage**, and **Locality**. Read `DEEPENING.md` when consolidating shallow modules or choosing dependency strategies. Read `DESIGN-IT-TWICE.md` when comparing alternative Interfaces.

## Core Model

A **Module** is anything with an Interface and an Implementation: a function, class, package, or tier-spanning slice. Its **Interface** includes the type surface plus invariants, ordering constraints, error modes, required configuration, and performance characteristics. An **Adapter** is a concrete implementation that satisfies an Interface at a **Seam**; use that term for role, not for internal behavior.

A deep Module puts substantial behavior behind a small Interface. Depth is leverage at the Interface, not an implementation-line ratio. **Leverage** is capability per unit of Interface callers learn. **Locality** is the concentration of change, bugs, knowledge, and verification behind one Interface.

## Design Rules

- Reduce methods and simplify parameters before adding abstractions.
- Hide complexity in the Implementation; keep callers dependent on the Interface.
- Use the deletion test: if deleting the Module makes complexity reappear across many callers, it earns its keep; if not, it may be a pass-through.
- Accept dependencies instead of constructing them when that improves testing or replaces a real dependency at a Seam.
- Return results instead of spreading side effects when the behavior can be expressed that way.
- Treat the Interface as the test surface. Tests should assert observable behavior through it, not implementation details.
- One Adapter means a hypothetical Seam. Two justified Adapters—typically production and test—make a real Seam. Do not add indirection for speculative variation.
- Prefer internal Seams over exposing test-only details through an external Interface.

## Application

Before changing a Module Interface, write down:

1. The smallest Interface callers and tests need.
2. The behavior and invariants hidden in the Implementation.
3. The Seam location and any concrete Adapters.
4. The dependency category and test strategy.
5. The leverage and locality gained.
6. The rejected simpler alternative and the result of the deletion test.

Preserve these decisions in the repository's Blueprint and Contracts. Use an ADR only when the decision is hard to reverse, surprising without explanation, and based on a meaningful trade-off.

Source adapted from Matt Pocock's `codebase-design` skill:
https://github.com/mattpocock/skills/tree/main/skills/engineering/codebase-design
