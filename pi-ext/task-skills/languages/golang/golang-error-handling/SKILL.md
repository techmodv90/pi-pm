---
name: golang-error-handling
description: "Idiomatic Golang error handling — creation, wrapping with %w, errors.Is/As, errors.Join, custom error types, sentinel errors, panic/recover, the single handling rule, structured logging with slog, HTTP request logging middleware, and samber/oops for production errors. Built to make logs usable at scale with log aggregation 3rd-party tools. Apply when creating, wrapping, inspecting, or logging errors in Go code. For samber/oops specifics → See `golang-samber-oops` skill; for slog handler ecosystem → See `golang-samber-slog` skill."
license: MIT
---

**Persona:** You are a Go reliability engineer. You treat every error as an event that must either be handled or propagated with context — silent failures and duplicate logs are equally unacceptable.

**Modes:**

- **Coding mode** — follow the best practices sequentially and inspect adjacent call sites for swallowed errors or log-and-return pairs.
- **Review mode** — focus on the diff: check ignored returns, missing wrapping context, log-and-return pairs, cleanup failures, and panic misuse.
- **Audit mode** — scan error creation, wrapping, single handling, cleanup, panic/recover, and structured logging as separate categories, then consolidate findings.

> **Community default.** A company skill that explicitly supersedes `golang-error-handling` skill takes precedence.

# Go Error Handling Best Practices

This skill guides the creation of robust, idiomatic error handling in Go applications. Follow these principles to write maintainable, debuggable, and production-ready error code.

## Best Practices Summary

1. **Every returned error is a first-class result and MUST be checked** — NEVER discard with `_`, ignore a returned error, or continue using result values before checking `err`
2. **Every checked error MUST have an explicit policy** — propagate it, translate it at a boundary, aggregate it, retry it with a bound, or record a genuinely non-actionable terminal cleanup failure; never silently discard it
3. **Errors MUST be wrapped with context** using `fmt.Errorf("{context}: %w", err)
3. **Error strings MUST be lowercase**, without trailing punctuation
4. **Use `%w` internally, `%v` at system boundaries** to control error chain exposure
5. **MUST use `errors.Is` for sentinel matching and `errors.As`/`errors.AsType` for typed chain inspection** instead of direct comparison or bare type assertions. For Go 1.26+, prefer `errors.AsType[T](err)` when `T` implements `error`; use `errors.As(err, &target)` for Go <1.26 or for non-error interface targets.
6. **SHOULD use `errors.Join`** (Go 1.20+) to combine independent errors
8. **Handle each error exactly once** — return/propagate it, translate it at a boundary, aggregate it, or log it at the terminal boundary; logging and then returning is not handling
9. **Never use `_` for an error result**. Even non-actionable cleanup failures must be observed and recorded at the terminal boundary
10. **Use sentinel errors** for expected conditions, custom types for carrying data
11. **NEVER use `panic` for expected error conditions** — reserve for truly unrecoverable states
12. **SHOULD use `slog`** (Go 1.21+) for structured error logging — not `fmt.Println` or `log.Printf`
13. **Use `samber/oops`** for production errors needing stack traces, user/tenant context, or structured attributes
14. **Log HTTP requests** with structured middleware capturing method, path, status, and duration
15. **Use log levels** to indicate error severity
16. **Never expose technical errors to users** — translate internal errors to user-friendly messages at the boundary while retaining technical details for logs
17. **Keep log grouping low-cardinality** — at logging/APM boundaries, keep message templates stable and attach IDs, paths, line numbers, and counts as structured attributes. Error values may include useful operational context, but avoid putting high-cardinality data into the stable log message used for grouping.

## Detailed Reference

- **[Error Creation](./references/error-creation.md)** — How to create errors that tell the story: error messages should be lowercase, no punctuation, and describe what happened without prescribing action. Covers sentinel errors (one-time preallocation for performance), custom error types (for carrying rich context), and the decision table for which to use when.

- **[Error Wrapping and Inspection](./references/error-wrapping.md)** — Why `fmt.Errorf("{context}: %w", err)` beats `fmt.Errorf("{context}: %v", err)` (chains vs concatenation). How to inspect chains with `errors.Is`, `errors.As`, and Go 1.26+ `errors.AsType` for type-safe error handling, and `errors.Join` for combining independent errors.

- **[Error Handling Patterns and Logging](./references/error-handling.md)** — The first-class error rule: every error is checked and receives an explicit policy; terminal logging, propagation, translation, aggregation, and panic/recover boundaries. Includes cleanup errors, `samber/oops`, and `slog` integration.

## Error Handling Audit Checklist

- Find discarded error returns, unchecked cleanup calls, result use before `err` checks, and overwritten errors.
- Audit `%w` versus `%v`, then verify `errors.Is` and `errors.As` work across intended boundaries.
- Find log-and-return duplication, swallowed errors, unbounded retries, and loops that continue after partial failure without recording it.
- Check `Close`, `Flush`, `Sync`, `Commit`, `Rollback`, `Rows.Err`, encoders, writers, and response writes.
- Audit panic/recover boundaries and structured logs for lost context or exposed secrets.
- Run the repository's error-focused linters, including `errcheck`, `errorlint`, `nilerr`, `rowserrcheck`, and `sqlclosecheck` when configured.

## Cross-References

- → See `golang-samber-oops` for full samber/oops API, builder patterns, and logger integration
- → See `golang-observability` for structured logging setup, log levels, and request logging middleware
- → See `golang-safety` for nil interface trap and nil error comparison pitfalls
- → See `golang-naming` for error naming conventions (ErrNotFound, PathError)

## References

- [lmittmann/tint](https://github.com/lmittmann/tint)
- [samber/oops](https://github.com/samber/oops)
- [samber/slog-multi](https://github.com/samber/slog-multi)
- [samber/slog-sampling](https://github.com/samber/slog-sampling)
- [samber/slog-formatter](https://github.com/samber/slog-formatter)
- [samber/slog-http](https://github.com/samber/slog-http)
- [samber/slog-sentry](https://github.com/samber/slog-sentry)
- [log/slog package](https://pkg.go.dev/log/slog)
