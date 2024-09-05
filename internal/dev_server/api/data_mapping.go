package api

import "github.com/launchdarkly/ldcli/internal/dev_server/model"

func availableVariationsToResponseFormat(availableVariations map[string][]model.Variation) map[string][]Variation {
	respAvailableVariations := make(map[string][]Variation, len(availableVariations))
	for flagKey, variationsForFlag := range availableVariations {
		respVariationsForFlag := make([]Variation, len(variationsForFlag))
		for _, variation := range variationsForFlag {
			respVariationsForFlag = append(respVariationsForFlag, Variation{
				Id:          variation.Id,
				Description: variation.Description,
				Name:        variation.Name,
				Value:       variation.Value,
			})
		}
		respAvailableVariations[flagKey] = respVariationsForFlag
	}
	return respAvailableVariations
}
