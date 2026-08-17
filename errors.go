//go:build wasm

package await

// Error represents an error returned by the await package.
type Error string

func (e Error) Error() string { return string(e) }

const (
	// ErrRejected wraps a Promise rejection with no usable message.
	ErrRejected Error = "await: promise rejected"
)
