package model

import "github.com/launchdarkly/go-sdk-common/v3/ldvalue"

type Variation struct {
	Id          string
	Description *string
	Name        *string
	Value       ldvalue.Value
}

type FlagVariation struct {
	FlagKey string
	Variation
}
