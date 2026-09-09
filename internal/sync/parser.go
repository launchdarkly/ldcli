package sync

type File struct {
	ProjectKey string
	RelPath    string
	Data       []byte
}

type Parser interface {
	Dir() string
	Accept(relPath string) bool
	Parse(file File) (SyncedResource, error)
}

func DefaultParsers() []Parser {
	return []Parser{
		variationParser{},
		toolParser{},
	}
}
