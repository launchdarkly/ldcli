package sync

import (
	"errors"
	"strings"
)

const MaxSourceIdentifierLength = 512

type SourceType string

const (
	SourceTypeGit   SourceType = "git"
	SourceTypeLocal SourceType = "local"
)

var (
	ErrInvalidSourceType       = errors.New("invalid source type")
	ErrInvalidSourceIdentifier = errors.New("invalid source identifier")
)

type Source struct {
	sourceType SourceType
	identifier string
}

func NewSource(sourceType SourceType, identifier string) (Source, error) {
	if sourceType != SourceTypeGit && sourceType != SourceTypeLocal {
		return Source{}, ErrInvalidSourceType
	}
	if identifier == "" ||
		identifier != strings.TrimSpace(identifier) ||
		len(identifier) > MaxSourceIdentifierLength {
		return Source{}, ErrInvalidSourceIdentifier
	}

	return Source{
		sourceType: sourceType,
		identifier: identifier,
	}, nil
}

func (source Source) Type() SourceType {
	return source.sourceType
}

func (source Source) Identifier() string {
	return source.identifier
}
