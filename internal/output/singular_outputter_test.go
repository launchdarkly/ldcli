package output_test

// func TestSingularOutputter_JSON(t *testing.T) {
// 	input := []byte(`{
// 		"key": "test-key",
// 		"name": "test-name",
// 		"other": "another-value"
// 	}`)
// 	output, err := output.CmdOutput(
// 		"json",
// 		output.NewSingularOutput(input),
// 	)

// 	require.NoError(t, err)
// 	assert.JSONEq(t, output, string(input))
// }

// func TestSingularOutputter_String(t *testing.T) {
// 	input := []byte(`{
// 		"key": "test-key",
// 		"name": "test-name",
// 		"other": "another-value"
// 	}`)
// 	expected := "test-name (test-key)"
// 	output, err := output.CmdOutput(
// 		"plaintext",
// 		output.NewSingularOutput(input),
// 	)

// 	require.NoError(t, err)
// 	assert.Equal(t, expected, output)
// }
