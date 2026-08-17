//go:build wasm

package await

import (
	"syscall/js"
)

// Promise blocks until p settles, via .then/.catch.
func Promise(p js.Value) (js.Value, error) {
	resultCh := make(chan js.Value, 1)
	errCh := make(chan error, 1)

	then := js.FuncOf(func(_ js.Value, args []js.Value) any {
		var val js.Value
		if len(args) > 0 {
			val = args[0]
		}
		resultCh <- val
		return js.Undefined()
	})
	defer then.Release()

	catch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		var errVal js.Value
		if len(args) > 0 {
			errVal = args[0]
		}
		msg := ""
		if !errVal.IsNull() && !errVal.IsUndefined() {
			msg = errVal.Call("toString").String()
		}
		if msg != "" {
			errCh <- Error(msg)
		} else {
			errCh <- ErrRejected
		}
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
