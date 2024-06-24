package model

import "context"

type Store interface {
	GetDevProjects(ctx context.Context) ([]string, error)
}
