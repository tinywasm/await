//go:build wasm

package await

import (
	"syscall/js"
)

// Event blocks the calling goroutine until target fires exactly one of the
// named JS events, then removes both listeners. Safe in WASM: the channel
// receive yields the goroutine and the JS event loop keeps running.
func Event(target js.Value, okEvent, failEvent string) (js.Value, error) {
	done := make(chan struct{}, 1)
	var result js.Value
	var err error

	var onOk, onFail js.Func
	onOk = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			result = args[0]
		}
		done <- struct{}{}
		return js.Undefined()
	})
	defer onOk.Release()

	onFail = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			errVal := args[0]
			if errVal.Truthy() {
				if msgVal := errVal.Get("message"); msgVal.Truthy() {
					err = Error(msgVal.String())
				} else if str := errVal.Call("toString"); str.Truthy() {
					err = Error(str.String())
				} else {
					err = ErrRejected
				}
			} else {
				err = ErrRejected
			}
		} else {
			err = ErrRejected
		}
		done <- struct{}{}
		return js.Undefined()
	})
	defer onFail.Release()

	defer func() {
		target.Call("removeEventListener", okEvent, onOk)
		target.Call("removeEventListener", failEvent, onFail)
	}()

	target.Call("addEventListener", okEvent, onOk)
	target.Call("addEventListener", failEvent, onFail)

	<-done
	return result, err
}
