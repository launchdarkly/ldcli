package api

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config oapi-codegen-cfg.yaml api.yaml
var _ StrictServerInterface = server{}

type server struct {
}

func NewStrictServer() StrictServerInterface {
	return server{}
}
