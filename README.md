# webtyp/await
<img src="docs/img/badges.svg">

[![Go Reference](https://pkg.go.dev/badge/github.com/webtyp/await.svg)](https://pkg.go.dev/webtyp.com/await)

Minimal JS async bridge for Go WASM: block a goroutine on a `Promise` or one-shot DOM event with zero dependencies.

## Overview

In Go WebAssembly applications, blocking on JS Promises or DOM events requires registering callbacks (`js.FuncOf`), yielding via a channel, and cleaning up listeners afterwards. `webtyp/await` provides clean, leak-free primitives with zero external dependencies.

`Event` is the shared underlying primitive that registers one-shot event listeners, handles resolution and error channels, and removes both listeners and releases callback references on return.

## Installation

```bash
go get webtyp.com/await
```

Note: All files require `//go:build wasm` and `syscall/js`.

## Usage

### Block on a JS Promise

`await.Promise` blocks until the `js.Value` Promise settles via `.then` or `.catch`.

```go
package main

import (
	"syscall/js"

	"webtyp.com/await"
)

func main() {
	p := js.Global().Get("fetch").Invoke("https://api.example.com/data")
	res, err := await.Promise(p)
	if err != nil {
		panic(err)
	}
	// res is the Response object
}
```

### Block on an IndexedDB Request

`await.Request` blocks until an IndexedDB request fires `"success"` or `"error"`, returning `req.Get("result")` on success or extracting the error message on failure.

```go
package main

import (
	"syscall/js"

	"webtyp.com/await"
)

func getRecord(store js.Value, key string) (js.Value, error) {
	req := store.Call("get", key)
	return await.Request(req)
}
```

### Block on a One-Shot DOM Event

`await.Event` blocks on any `js.Value` target exposing `addEventListener` / `removeEventListener` until either the success event or error event fires.

```go
package main

import (
	"syscall/js"

	"webtyp.com/await"
)

func awaitLoad(element js.Value) (js.Value, error) {
	return await.Event(element, "load", "error")
}
```

## License

MIT
