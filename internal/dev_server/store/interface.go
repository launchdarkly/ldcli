package store

type Store interface {
	GetDevProjects() ([]string, error)
}
