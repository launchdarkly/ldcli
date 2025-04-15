package model

import "fmt"

type ErrAlreadyExists struct {
	kind string
	key  string
}

func (e ErrAlreadyExists) Error() string {
	return fmt.Sprintf("%s %s already exists", e.kind, e.key)
}

func NewErrAlreadyExists(kind, key string) ErrAlreadyExists {
	return ErrAlreadyExists{
		kind: kind,
		key:  key,
	}
}
