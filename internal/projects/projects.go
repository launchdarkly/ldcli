package projects

import (
	"context"
	"encoding/json"

	ldapi "github.com/launchdarkly/api-client-go"
)

type getProjectsResponse struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func GetProjects() ([]byte, error) {
	config := ldapi.NewConfiguration()
	config.AddDefaultHeader("Authorization", "api-e8537cb2-1cd2-4d2e-8dea-4c4392fb5b1f")
	config.BasePath = "http://localhost:3000/api/v2"
	client := ldapi.NewAPIClient(config)
	response, _, err := client.ProjectsApi.GetProjects(context.Background())
	if err != nil {
		// 401 - should return unauthorized type error with body(?)
		// 404 - should return not found type error with body
		e, ok := err.(ldapi.GenericSwaggerError)
		if ok {
			return e.Body(), err
		}
		return nil, err
	}

	resp, err := json.Marshal(response.Items)
	if err != nil {
		return nil, err
	}

	// fmt.Println(">>> found projects", len(response.Items))
	// spew.Dump(response.Links)
	// for _, p := range response.Items {
	// 	fmt.Println(">>> found project", p.Key)
	// }

	return resp, nil
}
