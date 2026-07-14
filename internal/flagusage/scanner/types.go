package scanner

import sitter "github.com/smacker/go-tree-sitter"

type scanParsedFile struct {
	path string
	src  []byte
	tree *sitter.Tree
	lang *Language
}

type FlagReference struct {
	FlagKey         string `json:"flagKey"`
	FilePath        string `json:"filePath"`
	Line            int    `json:"line"`
	Column          int    `json:"column"`
	Kind            string `json:"kind"` // variation, hook, useFlags-property, wrapper-definition, wrapper-call
	Method          string `json:"method"`
	DefaultValue    string `json:"defaultValue,omitempty"`
	SurroundingCode string `json:"surroundingCode,omitempty"`
	WrapperName     string `json:"wrapperName,omitempty"`
	VariationType   string `json:"variationType,omitempty"`
}

type WrapperMapping struct {
	ExportName    string `json:"exportName"`
	FlagKey       string `json:"flagKey"`
	DefaultValue  string `json:"defaultValue"`
	VariationType string `json:"variationType,omitempty"`
	FilePath      string `json:"filePath"`
	Line          int    `json:"line"`
}

type ScanStats struct {
	FilesScanned    int            `json:"filesScanned"`
	ReferencesFound int            `json:"referencesFound"`
	UniqueFlags     int            `json:"uniqueFlags"`
	ByKind          map[string]int `json:"byKind"`
}

type ScanResult struct {
	References []FlagReference  `json:"references"`
	Wrappers   []WrapperMapping `json:"wrappers"`
	Stats      ScanStats        `json:"stats"`
}
