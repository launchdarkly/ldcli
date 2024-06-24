package api

import "context"

type Server struct{}

func NewStrictServer() Server {
	return Server{}
}

func (s Server) GetDevProjects(ctx context.Context, request GetDevProjectsRequestObject) (GetDevProjectsResponseObject, error) {
	//TODO implement me
	panic("implement me")
}

func (s Server) DeleteDevProjectsProjectKey(ctx context.Context, request DeleteDevProjectsProjectKeyRequestObject) (DeleteDevProjectsProjectKeyResponseObject, error) {
	//TODO implement me
	panic("implement me")
}

func (s Server) GetDevProjectsProjectKey(ctx context.Context, request GetDevProjectsProjectKeyRequestObject) (GetDevProjectsProjectKeyResponseObject, error) {
	//TODO implement me
	panic("implement me")
}

func (s Server) PostDevProjectsProjectKey(ctx context.Context, request PostDevProjectsProjectKeyRequestObject) (PostDevProjectsProjectKeyResponseObject, error) {
	//TODO implement me
	panic("implement me")
}

func (s Server) DeleteDevProjectsProjectKeyOverridesFlagKey(ctx context.Context, request DeleteDevProjectsProjectKeyOverridesFlagKeyRequestObject) (DeleteDevProjectsProjectKeyOverridesFlagKeyResponseObject, error) {
	//TODO implement me
	panic("implement me")
}

func (s Server) PutDevProjectsProjectKeyOverridesFlagKey(ctx context.Context, request PutDevProjectsProjectKeyOverridesFlagKeyRequestObject) (PutDevProjectsProjectKeyOverridesFlagKeyResponseObject, error) {
	//TODO implement me
	panic("implement me")
}
