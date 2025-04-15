package model

import "fmt"

type ErrNotFound struct {
	kind string
	key  string
}

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("%s %s not found", e.kind, e.key)
}

func NewErrNotFound(kind, key string) ErrNotFound {
	return ErrNotFound{
		kind: kind,
		key:  key,
	}
}
