package inbound

// Change is an optional field a PATCH may set: nothing changes unless Set; a nil Value clears it.
type Change[T any] struct {
	Set   bool
	Value *T
}
