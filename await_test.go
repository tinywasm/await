//go:build wasm

package await_test

import (
	"syscall/js"
	"testing"
	"time"

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
	if err.Error() != "Error: boom" {
		t.Fatalf("want 'Error: boom', got %q", err.Error())
	}
}

func TestPromise_reject_null(t *testing.T) {
	p := js.Global().Get("Promise").Call("reject", js.Null())
	_, err := await.Promise(p)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != await.ErrRejected {
		t.Fatalf("want ErrRejected, got %v", err)
	}
}

func newMockTarget() (target js.Value, listeners map[string]js.Value, addCount *int, removeCount *int) {
	listeners = make(map[string]js.Value)
	addC := 0
	removeC := 0

	obj := js.Global().Get("Object").New()
	obj.Set("addEventListener", js.FuncOf(func(_ js.Value, args []js.Value) any {
		addC++
		eventType := args[0].String()
		fn := args[1]
		listeners[eventType] = fn
		return nil
	}))
	obj.Set("removeEventListener", js.FuncOf(func(_ js.Value, args []js.Value) any {
		removeC++
		eventType := args[0].String()
		delete(listeners, eventType)
		return nil
	}))
	obj.Set("trigger", js.FuncOf(func(_ js.Value, args []js.Value) any {
		eventType := args[0].String()
		if fn, ok := listeners[eventType]; ok {
			if len(args) > 1 {
				fn.Invoke(args[1])
			} else {
				fn.Invoke()
			}
		}
		return nil
	}))

	return obj, listeners, &addC, &removeC
}

func TestEvent_success(t *testing.T) {
	target, _, addCount, removeCount := newMockTarget()

	go func() {
		time.Sleep(10 * time.Millisecond)
		target.Call("trigger", "load", "hello")
	}()

	res, err := await.Event(target, "load", "error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.String() != "hello" {
		t.Fatalf("want 'hello', got %q", res.String())
	}
	if *addCount != 2 {
		t.Fatalf("want 2 addEventListener calls, got %d", *addCount)
	}
	if *removeCount != 2 {
		t.Fatalf("want 2 removeEventListener calls, got %d", *removeCount)
	}
}

func TestEvent_fail(t *testing.T) {
	target, _, _, _ := newMockTarget()

	go func() {
		time.Sleep(10 * time.Millisecond)
		errObj := js.Global().Get("Object").New()
		errObj.Set("message", "event failed")
		target.Call("trigger", "error", errObj)
	}()

	_, err := await.Event(target, "load", "error")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "event failed" {
		t.Fatalf("want 'event failed', got %q", err.Error())
	}
}

func TestEvent_cleanup(t *testing.T) {
	target, listeners, _, _ := newMockTarget()

	go func() {
		time.Sleep(10 * time.Millisecond)
		target.Call("trigger", "load", "ok")
	}()

	_, err := await.Event(target, "load", "error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(listeners) != 0 {
		t.Fatalf("expected 0 remaining listeners, got %d", len(listeners))
	}

	// Triggering event again after return must not panic or invoke released js.Func
	target.Call("trigger", "load", "again")
}

func TestRequest_success(t *testing.T) {
	req, _, _, _ := newMockTarget()
	req.Set("result", 100)

	go func() {
		time.Sleep(10 * time.Millisecond)
		req.Call("trigger", "success")
	}()

	res, err := await.Request(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Int() != 100 {
		t.Fatalf("want 100, got %d", res.Int())
	}
}

func TestRequest_error(t *testing.T) {
	req, _, _, _ := newMockTarget()
	errObj := js.Global().Get("Object").New()
	errObj.Set("message", "IndexedDB constraint error")
	req.Set("error", errObj)

	go func() {
		time.Sleep(10 * time.Millisecond)
		req.Call("trigger", "error")
	}()

	_, err := await.Request(req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "IndexedDB constraint error" {
		t.Fatalf("want 'IndexedDB constraint error', got %q", err.Error())
	}
}

func TestRequest_error_default(t *testing.T) {
	req, _, _, _ := newMockTarget()
	req.Set("error", js.Null())

	go func() {
		time.Sleep(10 * time.Millisecond)
		req.Call("trigger", "error")
	}()

	_, err := await.Request(req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "unknown IndexedDB error" {
		t.Fatalf("want 'unknown IndexedDB error', got %q", err.Error())
	}
}
