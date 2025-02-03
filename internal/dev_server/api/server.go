package api

import "context"

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config oapi-codegen-cfg.yaml api.yaml
var _ StrictServerInterface = server{}

type server struct {
}

func (s server) GetBackup(ctx context.Context, request GetBackupRequestObject) (GetBackupResponseObject, error) {
	//TODO implement me
	panic("implement me")
}

func NewStrictServer() StrictServerInterface {
	return server{}
}
