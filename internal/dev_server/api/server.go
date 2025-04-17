package api

var _ StrictServerInterface = server{}

type server struct {
}

func NewStrictServer() StrictServerInterface {
	return server{}
}
