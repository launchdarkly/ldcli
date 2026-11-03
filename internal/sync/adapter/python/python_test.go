package python

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sitter "github.com/tree-sitter/go-tree-sitter"
	pythonlanguage "github.com/tree-sitter/tree-sitter-python/bindings/go"

	syncdomain "github.com/launchdarkly/ldcli/internal/sync"
	"github.com/launchdarkly/ldcli/internal/sync/adapter"
	"github.com/launchdarkly/ldcli/internal/sync/adapter/code"
)

func TestPythonInspectorHandlesStaticAndDynamicLanguageForms(t *testing.T) {
	source := []byte(`
from langchain_core.prompts import PromptTemplate, ChatPromptTemplate
from langchain_core.tools import tool
from fastmcp import FastMCP
from .values import TEXT, MISSING as missing

TRUE = True
FALSE = False
NUMBER = 1.5
TUPLE = ("system", TEXT)
OBJECT = {"text": TEXT, 1: "number", **{"extra": "value"}}
DYNAMIC = [TEXT, make_value()]
MUTATED = ["one"]
MUTATED.append("two")
MUTATED[0] = "changed"

empty = PromptTemplate.from_template()
chat = ChatPromptTemplate.from_messages([("human", TEXT), ("ai", "Done"), {"role": "system"}])

@tool
def search(query: str):
    """Search docs."""
    return query

mcp = FastMCP("server")
@mcp.tool(description="Run a task")
def run_task(value: str):
    """Run."""
    return value

@mcp.prompt()
def dynamic_prompt(name: str):
    """Prompt docs."""
    return f"Hello {name}"

class Scope:
    VALUE = "scoped"
`)
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: source},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte(`TEXT = "Hello {name}"`)},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, root)
	assert.NotNil(t, root.Fields["tools"])
	assert.NotNil(t, root.Fields["prompts"])

	dependencies, err := pythonAdapter{}.Dependencies(adapter.Document{Path: "src/main.py", Data: source})
	require.NoError(t, err)
	assert.Contains(t, dependencies, "src/values.py")
}

func TestPythonValueSafetyBranches(t *testing.T) {
	document := adapter.Document{Path: "main.py", Data: []byte(`
TRUE = True
FALSE = False
OVERFLOW = 1e9999
DICT = {"static": "value", "dynamic": make_value()}
LIST = ["value", make_value()]
message = AIMessage(content="done")
later = UNKNOWN
`)}
	tree, err := code.ParseTree(document, sitter.NewLanguage(pythonlanguage.Language()))
	require.NoError(t, err)
	defer tree.Close()

	trueNode := code.FindNode(tree.RootNode(), document.Data, "true", "True")
	require.NotNil(t, trueNode)
	value := pythonValue(trueNode, document.Data, nil)
	encoded, err := value.Encode(false)
	require.NoError(t, err)
	assert.Equal(t, "False", string(encoded))
	_, err = value.Encode("false")
	require.Error(t, err)

	overflow := code.FindNode(tree.RootNode(), document.Data, "float", "1e9999")
	require.NotNil(t, overflow)
	assert.Equal(t, adapter.CapabilityReadOnly, pythonValue(overflow, document.Data, nil).Capability)

	dictionary := code.FindNode(tree.RootNode(), document.Data, "dictionary", `{"static": "value", "dynamic": make_value()}`)
	require.NotNil(t, dictionary)
	assert.Equal(t, adapter.CapabilityReadOnly, pythonDictionary(dictionary, document.Data, nil).Capability)
	list := code.FindNode(tree.RootNode(), document.Data, "list", `["value", make_value()]`)
	require.NotNil(t, list)
	assert.Equal(t, adapter.CapabilityReadOnly, pythonSequence(list, document.Data, nil).Capability)

	call := code.FindNode(tree.RootNode(), document.Data, "call", `AIMessage(content="done")`)
	require.NotNil(t, call)
	message := pythonMessage(call, document.Data, nil)
	assert.Equal(t, syncdomain.RoleAssistant, message.Fields["role"].Value)
	assert.Equal(t, "done", message.Fields["content"].Value)

	identifier := code.FindNode(tree.RootNode(), document.Data, "identifier", "UNKNOWN")
	require.NotNil(t, identifier)
	wildcard := &adapter.Node{Reason: "unresolved", Code: syncdomain.ErrorImportUnresolved}
	resolved := pythonValue(identifier, document.Data, map[string]*adapter.Node{"*": wildcard})
	assert.Contains(t, resolved.Reason, "star import")

	assert.Equal(t, "module.py", pythonModulePaths("main.py", "module")[0])
	assert.Empty(t, pythonMutationRoot(nil, nil))
	assert.Equal(t, adapter.CapabilityReadOnly, pythonMessages(nil, nil, nil).Capability)
}

func TestCodeParsersRejectUnsafeInput(t *testing.T) {
	_, err := pythonAdapter{}.Dependencies(adapter.Document{Path: "main.py", Data: []byte{0xff}})
	require.Error(t, err)
	_, err = pythonAdapter{}.Dependencies(adapter.Document{Path: "main.py", Data: make([]byte, code.MaxSourceBytes+1)})
	require.Error(t, err)
	_, err = pythonAdapter{}.Dependencies(adapter.Document{Path: "main.py", Data: []byte("def broken(")})
	require.Error(t, err)

	result := code.MessageList(&adapter.Node{Items: []*adapter.Node{
		{Items: []*adapter.Node{{Value: "human"}, {Value: "Hello"}}},
		{Fields: map[string]*adapter.Node{"role": {Value: "system"}, "content": {Value: true}}},
	}})
	assert.Equal(t, adapter.CapabilityReadOnly, result.Capability)
	assert.Nil(t, code.MessageList(nil))

}
