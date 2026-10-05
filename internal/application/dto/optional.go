package dto

import (
	"bytes"
	"encoding/json"
)

// Optional is a field of a PATCH body (a JSON merge patch): Set says it was sent at all, Null that it was
// sent as null, Value is what was sent otherwise. encoding/json leaves it zero when the field is absent and
// calls UnmarshalJSON when it's present, null included.
type Optional[T any] struct {
	Set   bool
	Null  bool
	Value T
}

func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(data, &o.Value)
}
