# Error Handling Patterns and Logging

## First-Class Errors And Single Handling

Every returned error is a first-class result. Check it before using other results or continuing. After checking it, choose an explicit policy: propagate it with context, translate it at a boundary, aggregate it, retry it under a bound, or log it at the terminal boundary. Logging alone is not a recovery policy, and logging before returning creates duplicate ownership.

Never use `_` for an error result. Even when failure cannot change the primary result, observe it and record it at the terminal boundary so operational failures remain visible.

```go
// ✗ Bad — logs AND returns (duplicate ownership and noise)
func processOrder(id string) error {
    err := chargeCard(id)
    if err != nil {
        log.Printf("failed to charge card: %v", err)
        return fmt.Errorf("charging card: %w", err)
    }
    return nil
}

// ✓ Good — return with context, let the caller decide
func processOrder(id string) error {
    err := chargeCard(id)
    if err != nil {
        return oops.
            With("order_id", id).
            Wrapf(err, "charging card")
    }
    return nil
}

// ✓ Good — handle at the terminal boundary (HTTP handler, main, etc.)
func handleOrder(w http.ResponseWriter, r *http.Request) {
    err := processOrder(r.FormValue("id"))
    if err != nil {
        slog.Error("order failed", "error", err)
        http.Error(w, "internal error", http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusOK)
}
```

## Cleanup Errors

Deferred cleanup can fail too. Do not silently discard `Close`, `Flush`, `Sync`, `Commit`, or `Rollback` errors when they affect durability or correctness. A named return error can preserve the primary error while joining a cleanup error:

```go
func writeFile(path string, data []byte) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing %s: %w", path, closeErr))
		}
	}()
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
```

For cleanup whose failure cannot change the primary result, observe it at the terminal boundary rather than discarding it:

```go
if err := resp.Body.Close(); err != nil {
	slog.Warn("closing response body failed", "error", err)
}
```

Prefer a helper when the same cleanup policy repeats. Never apply a weaker policy to writes, transactions, synchronization, or durable storage without checking the API contract.

## Panic and Recover

### When to panic

Panic MUST only be used for truly unrecoverable states — programmer errors, impossible conditions, or corrupt invariants. NEVER use panic for expected failures like network timeouts or missing files.

```go
// ✓ Acceptable — programmer error in initialization
func MustCompileRegex(pattern string) *regexp.Regexp {
    re, err := regexp.Compile(pattern)
    if err != nil {
        panic(fmt.Sprintf("invalid regex %q: %v", pattern, err))
    }
    return re
}

// ✗ Bad — panic for a normal failure
func GetUser(id string) *User {
    user, err := db.Find(id)
    if err != nil {
        panic(err) // callers cannot recover gracefully
    }
    return user
}
```

### Recovering from panics

Use `recover` in deferred functions at goroutine boundaries (HTTP handlers, worker goroutines) to prevent one panic from crashing the entire process.

```go
func safeHandler(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if r := recover(); r != nil {
                slog.Error("panic recovered",
                    "panic", r,
                    "stack", string(debug.Stack()),
                )
                http.Error(w, "internal error", http.StatusInternalServerError)
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```

For structured panic recovery with `samber/oops`, see the `golang-samber-oops` skill.

## Why Use `samber/oops`

- **Stack traces** — you see `"connection refused"` but need to know where it originated
- **Structured context** — user ID, tenant ID, or request metadata attached to the error
- **Error codes** — machine-readable identifiers for monitoring dashboards
- **Public/private separation** — safe message to show end users
- ...

`samber/oops` is a **drop-in replacement** that fills these gaps. Every `oops` error implements the standard `error` interface, works with `errors.Is`/`errors.As`, and adds structured attributes:

```go
// ✗ Before — standard errors, no context
func (s *OrderService) CreateOrder(ctx context.Context, req CreateOrderReq) error {
    err := s.db.Insert(ctx, req.Order)
    if err != nil {
        return fmt.Errorf("inserting order: %w", err)
    }
    return nil
}

// ✓ After — samber/oops, rich context for debugging
func (s *OrderService) CreateOrder(ctx context.Context, req CreateOrderReq) error {
    err := s.db.Insert(ctx, req.Order)
    if err != nil {
        return oops.
            In("order-service").
            Code("order_insert_failed").
            User(req.UserID).
            With("order_id", req.Order.ID).
            Wrapf(err, "inserting order")
    }
    return nil
}
```

When this error is logged, you get the stack trace, user ID, order ID, domain, error code, and the full error chain — all structured and machine-parseable.

## Logging Errors with `slog`

→ See `golang-observability` skill for comprehensive structured logging guidance, including `slog` setup, log levels, log handlers, HTTP middleware, and cost considerations.
