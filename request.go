//go:build wasm

package await

import (
	"syscall/js"
)

// Request blocks until an IndexedDB request fires "success" or "error", and
// returns req.Get("result") on success. It is Event with IndexedDB's specific
// error-message extraction (req.Get("error").Get("message")).
func Request(req js.Value) (js.Value, error) {
	_, err := Event(req, "success", "error")
	if err != nil {
		errVal := req.Get("error")
		msg := "unknown IndexedDB error"
		if errVal.Truthy() && errVal.Get("message").Truthy() {
			msg = errVal.Get("message").String()
		}
		return js.Value{}, Error(msg)
	}
	return req.Get("result"), nil
}
