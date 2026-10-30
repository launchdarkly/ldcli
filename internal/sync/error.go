package sync

import (
	"errors"
	"fmt"
)

// ErrorCode identifies one stable sync failure.
type ErrorCode string

const (
	ErrorInvalidPathSyntax       ErrorCode = "invalid_path_syntax"
	ErrorPathNotFound            ErrorCode = "path_not_found"
	ErrorPathAmbiguous           ErrorCode = "path_ambiguous"
	ErrorPathTypeMismatch        ErrorCode = "path_type_mismatch"
	ErrorFilterTypeMismatch      ErrorCode = "filter_value_type_mismatch"
	ErrorMapTypeMismatch         ErrorCode = "map_value_type_mismatch"
	ErrorIndexOutOfRange         ErrorCode = "index_out_of_range"
	ErrorIndexedSelectionChanged ErrorCode = "indexed_selection_changed"
	ErrorUnsupportedFormat       ErrorCode = "unsupported_source_format"
	ErrorFieldReadOnly           ErrorCode = "field_read_only"
	ErrorDuplicateKey            ErrorCode = "duplicate_key"
	ErrorInvalidSource           ErrorCode = "invalid_source"
	ErrorInvalidReference        ErrorCode = "invalid_reference"
	ErrorDefinitionConflict      ErrorCode = "resource_definition_conflict"
	ErrorLinkedSourceMissing     ErrorCode = "linked_source_missing"
	ErrorSyncSettingUnavailable  ErrorCode = "sync_setting_unavailable"
	ErrorBindingStale            ErrorCode = "binding_stale"
	ErrorStateChanged            ErrorCode = "state_changed"
	ErrorPublicationUnsupported  ErrorCode = "publication_not_supported"
	ErrorOverlappingEdits        ErrorCode = "overlapping_edits"
	ErrorResourceDeleted         ErrorCode = "resource_deleted_in_launchdarkly"
	ErrorPlaceholderAmbiguous    ErrorCode = "placeholder_translation_ambiguous"
	ErrorImportUnresolved        ErrorCode = "import_unresolved"
)

// Problem describes one actionable sync error.
type Problem struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	Resource   string    `json:"resource,omitempty"`
	File       string    `json:"file,omitempty"`
	Line       int       `json:"line,omitempty"`
	Column     int       `json:"column,omitempty"`
	Suggestion string    `json:"suggestion,omitempty"`
	Cause      error     `json:"-"`
}

// Error returns the message without embedding the stable code.
func (problem *Problem) Error() string {
	if problem == nil {
		return ""
	}
	if problem.Suggestion == "" {
		return problem.Message
	}
	return problem.Message + ". " + problem.Suggestion
}

// Unwrap returns the lower-level cause.
func (problem *Problem) Unwrap() error {
	if problem == nil {
		return nil
	}
	return problem.Cause
}

// NewProblem creates one sync problem.
func NewProblem(code ErrorCode, message, suggestion string) *Problem {
	return &Problem{Code: code, Message: message, Suggestion: suggestion}
}

// ProblemFrom returns the first sync problem in an error chain.
func ProblemFrom(err error) (*Problem, bool) {
	var problem *Problem
	ok := errors.As(err, &problem)
	return problem, ok
}

// WithResource adds a resource identity to a problem.
func (problem *Problem) WithResource(id ResourceID) *Problem {
	if problem != nil {
		problem.Resource = id.Label()
	}
	return problem
}

// WithFile adds a source location to a problem.
func (problem *Problem) WithFile(file string, line, column int) *Problem {
	if problem != nil {
		problem.File, problem.Line, problem.Column = file, line, column
	}
	return problem
}

// WrapProblem adds a lower-level cause.
func WrapProblem(problem *Problem, cause error) *Problem {
	if problem != nil {
		problem.Cause = cause
		if problem.Message == "" && cause != nil {
			problem.Message = fmt.Sprint(cause)
		}
	}
	return problem
}
