# Codebase Design At Module Seams

**Status**: accepted

When Blueprint or Contracts introduce or change a Module Interface, Seam, Adapter, or cross-caller responsibility, planning must evaluate depth, locality, leverage, testability, and the deletion test. Prefer a small Interface with behavior hidden in one Implementation. Add a Seam only when behavior actually varies or callers need an independently testable change point; one implementation alone is not sufficient justification for a speculative abstraction. Task Graph decomposition must preserve these seams while keeping each Task independently verifiable.
