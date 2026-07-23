## Why

While Momo implements robust crash-safety with `defer recover()` blocks across its transport and common layers (satisfying Rule 4), several of these recovery blocks are currently silent: they assign an error to the named `err` return parameter but do not emit a `log.Printf` message to Stderr (violating **Rule 9: No Silent Failures**). To ensure complete, consistent system observability on failures, we must unify these blocks under our established two-line recovery pattern, resolving tracking issue #245.

## What Changes

- **Refactor Silent Recovery Blocks:** Add explicit `log.Printf` statements to all 18 silent recovery blocks in `src/transport/momo_tcp.go`, `src/transport/momo_quic.go`, and `src/common/crush.go`.
- **Unify Logger Imports:** Ensure the standard `"log"` package is correctly imported and utilized across all target source files.

## Capabilities

### New Capabilities
- `unified-panic-logging`: Observability suite ensuring all transport and placement recovery blocks emit trace-friendly, explicit logs upon catching a panic.

## Impact

- **Affected Code:**
  - `src/transport/momo_tcp.go`
  - `src/transport/momo_quic.go`
  - `src/common/crush.go`
- **APIs:** Does not alter any API signatures or protocol wire frames.
