---
PLAN: "feat: minimal JS async bridge — block a goroutine on a Promise or a one-shot DOM event"
TAG: v0.1.0
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 17528392684790549094
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# Plan — `tinywasm/await`, the JS async bridge

## 1. Why this module exists

The same ~35-line pattern — register two `js.FuncOf` callbacks, wait on a
channel, release both — is implemented **three times already** in this
ecosystem, byte-for-byte the same shape with cosmetic differences:

| Where | What it waits for |
|---|---|
| `tinywasm/jsvalue` (`async_wasm.go`) | a JS `Promise`, and separately an IndexedDB request |
| `tinywasm/indexdb` (`tx.go`, `processCursorRequest`) | a `success`/`error` pair on a request |
| `tinywasm/keyring/browser`, `tinywasm/webauthn` (planned) | the same two shapes, about to become a fourth and fifth copy |

Worse: **`indexdb` already imports `jsvalue`** and still keeps its own copy
instead of calling `jsvalue.AwaitRequest`. Three implementations of the same
~15 lines exist in two files that could import each other today. That is the
DRY violation this module exists to end — not a hypothetical one, a live one.

The reason nobody built this as its own module before is that `jsvalue` is not
a safe place for it: importing `jsvalue` pulls `tinywasm/fmt` and
`tinywasm/model`, which is too much weight for something as small as "block on
a promise" to carry into a size-sensitive package like `keyring/browser`.

**This module is the extraction, done right: zero dependencies**, so any
WASM-bound package can import it for the cost of the code it actually uses.

## 2. Ecosystem rules that apply here

- Every file is `//go:build wasm` — `syscall/js` does not compile without it.
- **`syscall/js` is the only import, anywhere in this module.** Not
  `tinywasm/fmt`, not `errors`. Precedent: importing `tinywasm/fmt` into
  `tinywasm/base64` cost 74 KB for one error declaration — this module exists
  specifically so packages avoid paying that cost for something this small.
  Errors are a local comparable string type, exactly like every other
  dependency-free package in this ecosystem.

## 3. Public API — implement exactly this surface

```go
package await

// Event blocks the calling goroutine until target fires exactly one of the
// named JS events, then removes both listeners. Safe in WASM: the channel
// receive yields the goroutine and the JS event loop keeps running.
//
// This is the one primitive the rest of the package is built from.
func Event(target js.Value, okEvent, failEvent string) (js.Value, error)

// Promise blocks until p settles, via .then/.catch. Equivalent to calling
// Event on a value that only fires "then"/"catch" — implemented directly
// because Promise has no addEventListener.
func Promise(p js.Value) (js.Value, error)

// Request blocks until an IndexedDB request fires "success" or "error", and
// returns req.Get("result") on success. It is Event with IndexedDB's specific
// error-message extraction (req.Get("error").Get("message")).
func Request(req js.Value) (js.Value, error)
```

Design notes:

- `Event` is generic over any `js.Value` exposing `addEventListener` — it does
  not know about IndexedDB. `Request` is a ~6-line wrapper around it that
  knows where the result and the error message live on that specific object
  shape. Do not merge them into one function with a mode flag; the two call
  shapes read cleanly separate at the call site (`await.Request(req)` vs.
  `await.Event(target, "load", "error")`) and that is worth the two names.
- `Promise` cannot be written in terms of `Event`: a `Promise` has `.then` /
  `.catch` methods, not `addEventListener`. It is its own ~20-line
  implementation, ported from `jsvalue.AwaitPromise` (§6).
- Every registered `js.Func` is `Release()`d via `defer` before the function
  returns, on every path, including inside the callbacks — a leaked `js.Func`
  is a permanent JS-side reference that never garbage-collects.

## 4. Errors

```go
type Error string
func (e Error) Error() string { return string(e) }

const (
	// ErrRejected wraps a Promise rejection with no usable message.
	ErrRejected Error = "await: promise rejected"
)
```

`Promise`'s rejection path extracts a string from the rejection value when
possible (`errVal.Call("toString").String()` when the value is truthy) and
falls back to `ErrRejected` otherwise — port this from `AwaitPromise` (§6)
rather than inventing a new message format; several `catch` sites elsewhere in
the ecosystem already expect the current wording.

`Request`'s error path mirrors `AwaitRequest` (§6): read
`req.Get("error").Get("message")` when `req.Get("error")` is truthy, else use
a fixed default. Keep the default's wording — `AwaitRequest`'s callers already
match against it in tests.

## 5. Files to create

| File | Contents |
|---|---|
| `await.go` | `Event` |
| `promise.go` | `Promise` |
| `request.go` | `Request` |
| `errors.go` | §4 |
| `tests/` | ported tests (§7) |

## 6. Source to port — do not reinvent it

Both existing implementations are already correct and exercised. Port them;
do not write new logic where a working copy exists.

### `jsvalue.AwaitPromise` and `jsvalue.AwaitRequest`

From `github.com/tinywasm/jsvalue` (this ecosystem, MIT), file
`async_wasm.go`. `AwaitPromise` becomes `Promise` verbatim except for the
error type (`fmt.Errf` → the local `ErrRejected`/message). `AwaitRequest`
becomes `Request`, expressed as a thin call into the new `Event`:

```go
// jsvalue/async_wasm.go, current source — port exactly, adjusting only errors
// (reproduced here so the executor does not need network access to fetch it)

//go:build wasm

package jsvalue

import (
	"syscall/js"

	"github.com/tinywasm/fmt"
)

func AwaitPromise(p js.Value) (js.Value, error) {
	resultCh := make(chan js.Value, 1)
	errCh := make(chan error, 1)

	then := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resultCh <- args[0]
		return js.Undefined()
	})
	defer then.Release()

	catch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		errVal := args[0]
		msg := "promise rejected"
		if !errVal.IsNull() && !errVal.IsUndefined() {
			msg = errVal.Call("toString").String()
		}
		errCh <- fmt.Errf("jsvalue: %s", msg)
		return js.Undefined()
	})
	defer catch.Release()

	p.Call("then", then).Call("catch", catch)
	select {
	case v := <-resultCh:
		return v, nil
	case err := <-errCh:
		return js.Value{}, err
	}
}

func AwaitRequest(req js.Value) (js.Value, error) {
	done := make(chan struct{}, 1)
	var result js.Value
	var reqErr error

	onSuccess := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		result = req.Get("result")
		done <- struct{}{}
		return nil
	})
	defer onSuccess.Release()

	onError := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		errVal := req.Get("error")
		msg := "unknown IndexedDB error"
		if errVal.Truthy() {
			msg = errVal.Get("message").String()
		}
		reqErr = fmt.Err("jsvalue: IndexedDB request failed:", msg)
		done <- struct{}{}
		return nil
	})
	defer onError.Release()

	req.Call("addEventListener", "success", onSuccess)
	req.Call("addEventListener", "error", onError)

	<-done
	return result, reqErr
}
```

**Generalise `AwaitRequest`'s body into `Event`**: the two-listener,
`addEventListener`/`done`-channel shape is identical between what
`AwaitRequest` does for `"success"`/`"error"` and what
`indexdb.processCursorRequest` does for the same two names (§6, second
snippet) — the only per-call difference is which two event names are used and
how the error message is pulled off the target. `Event` takes the event names
as parameters; `Request` calls `Event(req, "success", "error")` and then reads
`req.Get("result")` — the IndexedDB-specific part `Event` cannot know about.

### `indexdb.processCursorRequest`, for comparison only — do not port this one

From `github.com/tinywasm/indexdb`, file `tx.go`. Read it to see the same
two-listener shape confirmed a third time, but **do not port it**: it
re-invokes `onSuccess` on every `cursor.Call("continue")`, so it is a
multi-fire loop, not a one-shot wait. `Event`/`Request` are one-shot by
design (each listener fires once and both are released). Forcing the cursor
loop through `Event` would require re-registering listeners per iteration —
slower and no clearer. It stays exactly where it is, in `indexdb`.

```go
// indexdb/tx.go — reference only, not ported
func processCursorRequest(req js.Value, onNext func(cursor js.Value) bool) error {
	done := make(chan struct{})
	var err error
	var onSuccess js.Func
	onSuccess = js.FuncOf(func(this js.Value, args []js.Value) any {
		cursor := req.Get("result")
		if !cursor.Truthy() {
			close(done)
			return nil
		}
		if onNext(cursor) {
			cursor.Call("continue")
		} else {
			close(done)
		}
		return nil
	})
	defer onSuccess.Release()
	// ... error listener, identical shape to AwaitRequest's ...
	req.Call("addEventListener", "success", onSuccess)
	req.Call("addEventListener", "error", onError)
	<-done
	return err
}
```

## 7. Tests

Port `jsvalue/async_wasm_test.go` verbatim (adjusting the import and function
names):

```go
//go:build wasm

package await_test

import (
	"syscall/js"
	"testing"

	"github.com/tinywasm/await"
)

func TestPromise_resolve(t *testing.T) {
	p := js.Global().Get("Promise").Call("resolve", 42)
	v, err := await.Promise(p)
	if err != nil {
		t.Fatal(err)
	}
	if v.Int() != 42 {
		t.Fatalf("want 42, got %d", v.Int())
	}
}

func TestPromise_reject(t *testing.T) {
	p := js.Global().Get("Promise").Call("reject", js.Global().Get("Error").New("boom"))
	_, err := await.Promise(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

Add for `Event`/`Request`, using a plain `js.Value` object built with
`js.Global().Get("Object").New()` plus `Set("addEventListener", ...)` /
`Set("dispatchEvent", ...)` stand-ins, or a minimal `EventTarget` if the test
harness exposes one:

- `Event` resolves when the ok event fires, with the event's argument returned.
- `Event` returns an error when the fail event fires instead.
- `Request` returns `req.Get("result")` on a `"success"` dispatch.
- `Request` returns the `error.message` string on an `"error"` dispatch, and
  the fixed default when `error` is falsy.
- No `js.Func` leak: after `Event` returns, dispatching either event again does
  nothing observable (the listeners were removed) — assert this by counting
  invocations through a closure.

Run with `gotest -tinygo`.

## 8. Documentation

`README.md` — the three functions, a ten-line example of `Promise` and one of
`Request`, and one sentence explaining why `Event` exists as the shared
primitive (§3) so a reader is not tempted to reinvent a fourth copy of this
pattern.

## 9. Consumers — coordinate, do not dispatch blind

These repositories switch to this module in the same wave (each has its own
plan referencing this one):

- `https://github.com/tinywasm/jsvalue/blob/main/docs/PLAN.md` — deletes
  `AwaitPromise`/`AwaitRequest`; jsvalue becomes codec-only.
- `https://github.com/tinywasm/indexdb/blob/main/docs/PLAN.md` — replaces
  `jsvalue.AwaitRequest` call sites with `await.Request`.
- `https://github.com/tinywasm/keyring/blob/main/docs/PLAN_STAGE_5_BROWSER.md`
  — imports this module instead of copying the pattern.
- `https://github.com/tinywasm/webauthn/blob/main/docs/PLAN.md` — same.

`goflare` (`r2/bucket.go`, `d1/adapter.go`) calls `jsvalue.AwaitPromise` today
and is **out of scope for this wave** — its one-line follow-up
(`jsvalue.AwaitPromise(x)` → `await.Promise(x)`, plus the import) is noted in
the master plan but not dispatched here.
