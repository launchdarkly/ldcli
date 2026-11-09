package adapter

import (
	"fmt"
	"slices"
	"strings"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
)

// NormalizeReference validates a descriptor and fills conventional defaults.
func NormalizeReference(id syncdomain.ResourceID, reference syncdomain.Reference) (syncdomain.Reference, error) {
	if reference.File == "" {
		return syncdomain.Reference{}, invalidReference(id, "file is required", "Pass --file.")
	}
	registered, err := lookup(reference.Format)
	if err != nil {
		return syncdomain.Reference{}, err
	}
	defaultSelect, defaultMap := registered.Defaults(id.Kind)
	if reference.Select == "" {
		if defaultSelect == "" {
			return syncdomain.Reference{}, invalidReference(
				id, fmt.Sprintf("select is required for format %q", reference.Format), "Pass --select.",
			)
		}
		reference.Select = defaultSelect
	}
	if err := ParsePath(reference.Select); err != nil {
		return syncdomain.Reference{}, annotateProblem(
			fmt.Errorf("select %q: %w", reference.Select, err), id, reference.File,
		)
	}
	if len(reference.Map) == 0 {
		if len(defaultMap) == 0 {
			return syncdomain.Reference{}, invalidReference(
				id, "map is required", "Pass at least one --map field=path value.",
			)
		}
		reference.Map = defaultMap
	}
	for target := range reference.Map {
		if err := ParsePath(target); err != nil {
			return syncdomain.Reference{}, annotateProblem(
				fmt.Errorf("map target %q: %w", target, err), id, reference.File,
			)
		}
	}
	fields, err := syncdomain.ProjectionFields(reference)
	if err != nil {
		return syncdomain.Reference{}, invalidReference(id, err.Error(), "Use a field valid for this resource kind.")
	}
	wholeParameters := false
	parameterKeys := map[string]syncdomain.Field{}
	for _, field := range fields {
		if field == syncdomain.FieldTools || field == syncdomain.FieldSkills {
			return syncdomain.Reference{}, invalidReference(
				id,
				fmt.Sprintf("map key %q is not allowed", field),
				fmt.Sprintf("List %s keys under top-level %q.", field, field),
			)
		}
		if !fieldAllowed(id.Kind, field) {
			return syncdomain.Reference{}, invalidReference(
				id,
				fmt.Sprintf("field %q is not valid for %s", field, id.Kind),
				"Use a mapped field supported by this resource kind.",
			)
		}
		if field == syncdomain.FieldModelParameters {
			wholeParameters = true
		}
		if key, parameter := syncdomain.ModelParameterKey(field); parameter {
			if key == "tools" {
				return syncdomain.Reference{}, invalidReference(
					id,
					fmt.Sprintf("map target %q duplicates the tools field", field),
					"List tool keys under top-level \"tools\".",
				)
			}
			if previous, duplicate := parameterKeys[key]; duplicate {
				return syncdomain.Reference{}, invalidReference(
					id,
					fmt.Sprintf("map targets %q and %q select the same model parameter", previous, field),
					"Map each model parameter key once.",
				)
			}
			parameterKeys[key] = field
		}
		if err := ParsePath(reference.Map[string(field)]); err != nil {
			return syncdomain.Reference{}, annotateProblem(
				fmt.Errorf("map field %q: %w", field, err), id, reference.File,
			)
		}
	}
	if wholeParameters && len(parameterKeys) != 0 {
		return syncdomain.Reference{}, invalidReference(
			id,
			"map target \"model.parameters\" overlaps an individual model parameter target",
			"Map the full parameters object or individual parameter keys.",
		)
	}
	return reference, nil
}

func invalidReference(id syncdomain.ResourceID, message, suggestion string) error {
	return syncdomain.NewProblem(
		syncdomain.ErrorInvalidReference, message, suggestion,
	).WithResource(id)
}

func fieldAllowed(kind syncdomain.Kind, field syncdomain.Field) bool {
	switch kind {
	case syncdomain.KindVariation:
		if _, parameter := syncdomain.ModelParameterKey(field); parameter {
			return true
		}
		return slices.Contains([]syncdomain.Field{
			syncdomain.FieldPrompt, syncdomain.FieldMessages, syncdomain.FieldName, syncdomain.FieldModel,
			syncdomain.FieldModelParameters,
		}, field)
	case syncdomain.KindTool:
		return slices.Contains([]syncdomain.Field{
			syncdomain.FieldDescription, syncdomain.FieldSchema,
			syncdomain.FieldCustomParameters, syncdomain.FieldTags,
		}, field)
	case syncdomain.KindSkill:
		return field == syncdomain.FieldDescription || field == syncdomain.FieldMarkdown
	default:
		return false
	}
}

// annotateProblem adds the resource and the file to the problem in an error.
// A wrapper such as `select "x": ...` names the part of the reference that
// failed, and the output prints the problem alone, so the problem takes that
// context into its message.
func annotateProblem(err error, id syncdomain.ResourceID, file string) error {
	if err == nil {
		return nil
	}
	problem, ok := syncdomain.ProblemFrom(err)
	if !ok {
		return err
	}
	if err != error(problem) {
		if prefix, found := strings.CutSuffix(err.Error(), ": "+problem.Error()); found && prefix != "" {
			problem.Message = prefix + ": " + problem.Message
		}
	}
	return problem.WithResource(id).WithFile(file, 0, 0)
}
