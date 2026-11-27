# Go (Golang) Specific Mutation Operators

This document covers mutation operators specific to Go beyond the universal arithmetic, conditional, and boolean operators in `SKILL.md`, with special emphasis on systems programming, concurrency, error handling, and Momo's **⚡ Bolt** & **🛡️ Sentinel** invariants.

---

## 1. Error Handling & POSIX Errno Mutations (`err != nil`, `errors.Is`, `syscall.*`)

Go uses explicit error value returns rather than exceptions. The most common surviving mutants in Go code live in error-handling branches.

| Original | Mutated | Test Should Verify |
|----------|---------|-------------------|
| `if err != nil { return err }` | `if false { return err }` (dropped error check) | Failure path returns non-nil error and aborts side effects |
| `return fmt.Errorf("...: %w", syscall.EACCES)` | `return fmt.Errorf("...: %w", syscall.EIO)` | Exact wrapped POSIX errno via `errors.Is(err, syscall.EACCES)` |
| `return fmt.Errorf("...: %w", err)` | `return fmt.Errorf("...: %v", err)` | Error chain unwrapping (`%w` vs `%v`) using `errors.Is` / `errors.As` |
| `errors.Is(err, syscall.ENOENT)` | `err == syscall.ENOENT` | Wrapped error still matches `errors.Is` |

**Example Analysis:**

```go
// Production code
func ValidateObjectKey(key string) error {
	if strings.Contains(key, "..") {
		return fmt.Errorf("invalid key %q: %w", key, syscall.EACCES)
	}
	return nil
}

// Mutant 1: `%v` instead of `%w`
// Mutant 2: `syscall.EINVAL` instead of `syscall.EACCES`

// ❌ WEAK TEST — Survives both Mutant 1 and Mutant 2!
func TestValidateObjectKey_Weak(t *testing.T) {
	err := ValidateObjectKey("../etc/passwd")
	if err == nil {
		t.Fatal("expected error")
	}
}

// ✅ STRONG TEST — Kills both Mutant 1 and Mutant 2!
func TestValidateObjectKey_Strong(t *testing.T) {
	err := ValidateObjectKey("../etc/passwd")
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("got err=%v, want errors.Is(_, syscall.EACCES)", err)
	}
}
```

---

## 2. Defer, Panic Recovery & Resource Cleanup Mutations

| Original | Mutated | Test Should Verify |
|----------|---------|-------------------|
| `defer common.RecoverErr("op", &err)` | (removed) | Panicking input recovers cleanly and returns `syscall.EIO` instead of crashing process |
| `defer common.RecoverErrClose("op", &err, conn)` | `defer common.RecoverErr("op", &err)` | Underlying `net.Conn` / `io.Closer` is closed on panic (Rule 43) |
| `defer conn.Close()` | (removed) | Resource is closed on both happy path and early-error return |
| `defer cancel()` | (removed) | Context timer/goroutine does not leak (`defer goleak.VerifyNone(t)`) |

**Example Analysis:**

```go
// Production code
func HandleStream(conn net.Conn) (err error) {
	defer common.RecoverErrClose("HandleStream", &err, conn)
	// ...
}

// Mutant: `RecoverErr` used instead of `RecoverErrClose` (conn left open on panic)

// ✅ STRONG TEST — Verifies both POSIX error return AND connection closure on panic
func TestHandleStream_PanicClosesConn(t *testing.T) {
	defer goleak.VerifyNone(t)
	spyConn := &closeSpyConn{ triggerPanicOnRead: true }
	err := HandleStream(spyConn)
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("expected syscall.EIO on panic recovery, got %v", err)
	}
	if !spyConn.closed {
		t.Fatal("expected connection to be closed on panic recovery")
	}
}
```

---

## 3. Slice, Map & Buffer Boundary Mutations

| Original | Mutated | Test Should Verify |
|----------|---------|-------------------|
| `buf[:n]` | `buf[:n-1]` or `buf[:]` | Exact byte slice length and trailing byte exclusion |
| `len(name) > MaxKeyLen` | `len(name) >= MaxKeyLen` | Boundary test at `len(name) == MaxKeyLen` (e.g., exactly 64 bytes) |
| `count < 0 \|\| count > MaxItems` | `count > MaxItems` | Negative length guard before `make([]T, count)` (Rule 32) |
| `delete(m, key)` | (removed) | Map entry is actually evicted and length decrements |
| `v, ok := m[k]; if !ok` | `v := m[k]; if v == zero` | Distinguishes missing key from present key with zero value |

**Example Analysis — Zero-Value vs Missing Key in Map:**

```go
// Production code
func GetReplicaWeight(weights map[string]int, nodeID string) int {
	if w, ok := weights[nodeID]; ok {
		return w
	}
	return DefaultWeight // 10
}

// Mutant: `if w := weights[nodeID]; w != 0 { return w }`

// ❌ WEAK TEST — Only tests positive weight and missing node
// ✅ STRONG TEST — Tests node explicitly configured with weight 0
func TestGetReplicaWeight_ExplicitZero(t *testing.T) {
	weights := map[string]int{"node-0": 0}
	if got := GetReplicaWeight(weights, "node-0"); got != 0 {
		t.Fatalf("got %d, want 0 for explicitly zero-weighted node", got)
	}
}
```

---

## 4. Integer Division, Bitwise & Overflow Mutations (Systems / Bolt Hot Paths)

| Original | Mutated | Test Should Verify |
|----------|---------|-------------------|
| `a << b` | `a >> b` | Bitshift direction with non-zero shift (`b > 0`) |
| `a & b` | `a \| b` or `a ^ b` | Bitmask filtering with mixed 0/1 bits |
| `a &^ b` (bit clear) | `a & b` | Specific cleared bit is 0 while unmasked bits stay 1 |
| `counter&127 == 0` | `counter&127 != 0` | Amortized deadline syscall fires once every 128 chunks, not 127/128 |
| `uint64(a)` overflow check | `<` vs `<=` at `math.MaxInt64` | Exact boundary at `math.MaxInt64` (`9223372036854775807`) and `math.MinInt64` |

---

## 5. Concurrency, Context & Channel Mutations

| Original | Mutated | Test Should Verify |
|----------|---------|-------------------|
| `case <-ctx.Done(): return ctx.Err()` | (branch removed) | Cancelled context unblocks immediately and returns `context.Canceled` / `DeadlineExceeded` |
| `conn.SetDeadline(time.Now().Add(d))` | (removed) | Slow/stalled peer times out with `syscall.ETIMEDOUT` |
| `mu.Lock(); defer mu.Unlock()` | (removed) | Concurrent callers running under `go test -race` trigger race detector if lock is removed |
| `atomic.AddInt64(&n, 1)` | `n++` | Concurrent subtests under `go test -race` detect unsynchronized mutation |

**Key Rule for Go Concurrency Mutants:**
Always run mutation validation and branch tests with `go test -race` and `defer goleak.VerifyNone(t)`. A concurrency mutant that removes a mutex or context cancellation channel is only killed if a test actually exercises concurrent contention or context cancellation!

---

## 6. Type Assertion, Type Switch & Nil-Slice vs Empty-Slice Mutations

| Original | Mutated | Test Should Verify |
|----------|---------|-------------------|
| `v, ok := x.(MyType); if !ok` | `v := x.(MyType)` (panics on wrong type) | Non-matching interface value returns clean error without panicking |
| `case *net.OpError:` in `switch` | (case branch removed) | Specific concrete error type takes dedicated branch rather than `default:` |
| `return []string{}` (non-nil empty) | `return nil` | JSON serialization (`[]` vs `null`) or caller nil-check where contract requires non-nil slice |
| `strings.HasPrefix(s, p)` | `strings.HasSuffix(s, p)` / `strings.Contains(s, p)` | Input where prefix `p` also appears in middle/end kills `Contains`/`HasSuffix` mutant |
| `bytes.TrimRight(b, "\x00")` | `bytes.Trim(b, "\x00")` | Input with leading `\x00` byte verifies only trailing padding is stripped |

---

## 7. Go-Specific Equivalent Mutants (Don't Chase False Survivors)

Some mutations in Go cannot be killed because the Go runtime or standard library guarantees identical behavior:
1. **`len(s) == 0` vs `len(s) <= 0`**: In Go, `len()` on a slice, string, map, or channel is never negative. Mutating `== 0` to `<= 0` produces an **equivalent mutant**.
2. **`make([]T, 0, cap)` capacity mutations**: Mutating pre-allocated capacity `make([]T, 0, 16)` to `make([]T, 0, 8)` does not change functional output (only heap allocations). Kill capacity mutants with an allocation benchmark/test using `testing.AllocsPerRun(100, fn) == 0` when zero-alloc (⚡ Bolt) is part of the contract!
3. **`delete(m, k)` on nil or absent key**: A guard `if _, ok := m[k]; ok { delete(m, k) }` mutated to bare `delete(m, k)` is functionally equivalent unless a counter/hook runs inside the `if` block.

---

## 8. Go-Specific Red Flags (Likely Surviving Mutants)

- [ ] Tests assert `if err == nil { t.Fatal(...) }` without checking `errors.Is(err, syscall.E...)`
- [ ] Tests only check happy-path map lookups (`m[k]`) and never test missing keys or keys mapped to zero-value `0`/`""`
- [ ] Slice boundary tests check `len == 10` against a `Max == 64` limit, never testing `64` and `65`
- [ ] Prefix/suffix tests use symmetric inputs (e.g., `"aba"`) where `HasPrefix` and `HasSuffix` both return `true`
- [ ] Panic recovery helpers (`defer common.RecoverErrClose`) have no test injecting a panic to prove recovery + resource closure
- [ ] Concurrent code is tested sequentially without `-race` or `goleak.VerifyNone(t)`
- [ ] Zero-allocation hot paths have no `testing.AllocsPerRun` or benchmark alloc assertion to catch buffer-capacity mutants

