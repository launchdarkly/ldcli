package model_test

import (
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/launchdarkly/go-server-sdk/v7/interfaces/flagstate"
	"github.com/launchdarkly/ldcli/internal/dev_server/model"
	"github.com/stretchr/testify/assert"
)

func TestFromAllFlags(t *testing.T) {
	sdkFlags := flagstate.NewAllFlagsBuilder().
		AddFlag("boolFlag", flagstate.FlagState{Value: ldvalue.Bool(true), Version: 1}).
		AddFlag("stringFlag", flagstate.FlagState{Value: ldvalue.String("cool"), Version: 1}).
		AddFlag("intFlag", flagstate.FlagState{Value: ldvalue.Int(123), Version: 1}).
		AddFlag("doubleFlag", flagstate.FlagState{Value: ldvalue.Float64(99.99), Version: 1}).
		AddFlag("jsonFlag", flagstate.FlagState{Value: ldvalue.CopyArbitraryValue(map[string]any{"cat": "hat"}), Version: 1}).
		Build()

	flagState := model.FromAllFlags(sdkFlags)
	expectedVersion := 1

	for key, state := range flagState {
		var expectedVal ldvalue.Value

		switch key {
		case "boolFlag":
			expectedVal = ldvalue.Bool(true)
		case "stringFlag":
			expectedVal = ldvalue.String("cool")
		case "intFlag":
			expectedVal = ldvalue.Int(123)
		case "doubleFlag":
			expectedVal = ldvalue.Float64(99.99)
		case "jsonFlag":
			expectedVal = ldvalue.CopyArbitraryValue(map[string]any{"cat": "hat"})
		}

		assert.Equal(t, expectedVersion, state.Version)
		assert.True(t, expectedVal.Equal(state.Value))
	}
}
