package python

import (
	"strings"
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
	assert.Empty(t, pythonMutatedName(nil, " ", nil, nil))
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

func TestPythonCollectionsTreatNoneAsStaticAndDynamicEntriesAsReadOnly(t *testing.T) {
	document := adapter.Document{Path: "main.py", Data: []byte(`
NONE = [None, "text"]
SPLAT = {"a": "b", **extra}
NUMERIC = {1: "a"}
`)}
	tree, err := code.ParseTree(document, sitter.NewLanguage(pythonlanguage.Language()))
	require.NoError(t, err)
	defer tree.Close()

	list := code.FindNode(tree.RootNode(), document.Data, "list", `[None, "text"]`)
	require.NotNil(t, list)
	value := pythonValue(list, document.Data, nil)
	assert.Equal(t, adapter.CapabilityDirect, value.Capability)
	assert.Equal(t, []any{nil, "text"}, value.Value)
	encoded, err := value.Encode([]any{nil, "next"})
	require.NoError(t, err)
	assert.Equal(t, `[None, "next"]`, string(encoded))

	for _, source := range []string{`{"a": "b", **extra}`, `{1: "a"}`} {
		dictionary := code.FindNode(tree.RootNode(), document.Data, "dictionary", source)
		require.NotNil(t, dictionary, source)
		result := pythonValue(dictionary, document.Data, nil)
		assert.Equal(t, adapter.CapabilityReadOnly, result.Capability, source)
		assert.Nil(t, result.Value, source)
	}
}

func TestMessageListsDoNotPanicOnMalformedItems(t *testing.T) {
	for name, items := range map[string][]*adapter.Node{
		"scalar item":   {{Value: 1.0}},
		"one-item pair": {{Items: []*adapter.Node{{Value: "human"}}}},
		"other fields":  {{Fields: map[string]*adapter.Node{"text": {Value: "x"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			result := code.MessageList(&adapter.Node{Items: items})
			require.NotNil(t, result)
			assert.Equal(t, adapter.CapabilityReadOnly, result.Capability)
			translated := code.TranslateFields(
				map[string]*adapter.Node{"messages": {Items: items}}, code.BracePlaceholders, "messages",
			)["messages"]
			assert.Equal(t, adapter.CapabilityReadOnly, translated.Capability)
			assert.Equal(t, "dynamic message content is read-only", translated.Reason)
		})
	}
}

func TestPythonImportedConstantsResolveRegardlessOfPosition(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(
			"from agents import Agent\nfrom .values import TEXT\nsupport = Agent(instructions=TEXT)\n",
		)},
		Dependencies: map[string]adapter.Document{
			// The constant ends after the use in the primary file.
			"src/values.py": {Path: "src/values.py", Data: []byte("# " + strings.Repeat("x", 300) + "\nTEXT = \"Hello\"\n")},
		},
	})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, "Hello", instructions.Value)
	assert.Equal(t, "src/values.py", instructions.File)
}

func TestPythonImportedConstantsBindFromTheirStatementOn(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
early = Agent(instructions=TEXT)
from .values import TEXT
late = Agent(instructions=TEXT)

def build():
    return Agent(instructions=TEXT)

def local():
    before = Agent(instructions=LOCAL)
    from .values import LOCAL
    return before
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Hello\"\nLOCAL = \"Local\"\n")},
		},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 4)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["early"].Fields["instructions"].Capability, "a use above the import is unbound")
	assert.Equal(t, "Hello", agents["late"].Fields["instructions"].Value, "a use after the import resolves")
	assert.Equal(t, "Hello", agents["build.Agent"].Fields["instructions"].Value, "a function body runs after the module imports")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["local.before"].Fields["instructions"].Capability, "a use above a local import is unbound")
}

func TestPythonClassBodyLambdasAndComprehensionsSeeModuleNames(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

class Team:
    TEXT = "class"
    direct = Agent(instructions=TEXT)
    build = lambda: Agent(instructions=TEXT)
    names = [Agent(instructions=TEXT) for _ in range(1)]
`)}})
	require.NoError(t, err)
	agents := map[string]string{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent.Fields["instructions"].Value.(string)
	}
	require.Len(t, agents, 3)
	assert.Equal(t, "class", agents["Team.direct"], "class body code sees the class attribute")
	for id, instructions := range agents {
		if id != "Team.direct" {
			assert.Equal(t, "module", instructions, "%s is in a scope of its own, which skips the class body", id)
		}
	}
}

func TestPythonUnpackingAssignmentsBindTheirNames(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
LIMIT = "kept"
ITEMS = ["a"]

def build(pairs):
    TEXT, other = pairs
    return Agent(instructions=TEXT)

plain = Agent(instructions=TEXT, name=LIMIT)
LIMIT, rest = values()
ITEMS[0], last = values()
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["build.Agent"].Fields["instructions"].Capability, "the unpacking binds a local that hides the module constant")
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["plain"].Fields["name"].Capability, "a module unpacking rebinds the constant")
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityDirect, constants["TEXT"].Capability, "a local unpacking does not touch the module constant")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["LIMIT"].Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, constants["ITEMS"].Capability, "a subscript target in an unpacking mutates the constant")
}

func TestPythonNamesImportedTwiceAreNotStatic(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
try:
    from .values import TEXT
except ImportError:
    from .fallback import TEXT
support = Agent(instructions=TEXT)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py":   {Path: "src/values.py", Data: []byte("TEXT = \"Values\"\n")},
			"src/fallback.py": {Path: "src/fallback.py", Data: []byte("TEXT = \"Fallback\"\n")},
		},
	})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, adapter.CapabilityReadOnly, instructions.Capability, "two imports bind the name, so no one module is its value")
	assert.Contains(t, instructions.Reason, "imported more than once")
}

func TestPythonLaterLocalsHideOuterConstantsInTheWholeFunction(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

def build():
    prompt = TEXT
    TEXT = "local"
    return Agent(instructions=prompt)

def unpack(pairs):
    prompt = TEXT
    TEXT, other = pairs
    return Agent(instructions=prompt)

plain = Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.NotEqual(t, "module", agents["build.Agent"].Fields["instructions"].Value, "a later assignment makes the name local, so the earlier use is unbound")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["build.Agent"].Fields["instructions"].Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["unpack.Agent"].Fields["instructions"].Capability, "a later unpacking makes the name local too")
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value)
}

func TestPythonCollectionsOfMutatedImportsAreReadOnly(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from .values import ITEMS, TEXT
ITEMS.append("b")
WRAPPED = [ITEMS]
MAPPED = {"items": ITEMS}
PLAIN = [TEXT]
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("ITEMS = [\"a\"]\nTEXT = \"Hello\"\n")},
		},
	})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["WRAPPED"].Capability, "a list that holds a mutated import is read-only")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["MAPPED"].Capability, "a dictionary that holds a mutated import is read-only")
	assert.Equal(t, adapter.CapabilityDirect, constants["PLAIN"].Capability, "a list of a plain import stays writable")
}

func TestPythonClassBodyImportsBindOnTheClassOnly(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from crewai import Agent

class Team:
    from agents import Agent
    writer = Agent(instructions="class")

    def build(self):
        return Agent(role="Writer", goal="Write", backstory="Years")
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.NotNil(t, agents["Team.writer"])
	assert.Equal(t, "class", agents["Team.writer"].Fields["instructions"].Value, "class body code sees the class import")
	// The CrewAI agent of the method takes its role as its id.
	require.NotNil(t, agents["Writer"], "a method resolves the name through the module, not the class")
	assert.Equal(t, "Years", agents["Writer"].Fields["backstory"].Value)
	assert.Nil(t, agents["Team.writer"].Fields["backstory"])
}

func TestPythonLoopTargetsShadowImportsFromTheirPointOn(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT
first = Agent(instructions=TEXT)
for TEXT in ["x"]:
    pass
later = Agent(instructions=TEXT)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Hello\"\n")},
		},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Hello", agents["first"].Fields["instructions"].Value, "the import is visible before the loop rebinds the name")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["later"].Fields["instructions"].Capability, "the loop target hides the import from the loop on")
}

func TestPythonFrameworkImportsWithAFallbackAreAmbiguous(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
try:
    from agents import Agent
except ImportError:
    from crewai import Agent
support = Agent(instructions="Help", backstory="Years")
`)}})
	require.NoError(t, err)
	assert.Empty(t, root.Fields["agents"].Items, "either import can run, so the name has no one framework")

	// A try statement at the start of the file is a fallback too.
	root, err = parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(
		"try:\n    from agents import Agent\nexcept ImportError:\n    from crewai import Agent\nsupport = Agent(instructions=\"Help\", backstory=\"Years\")\n",
	)}})
	require.NoError(t, err)
	assert.Empty(t, root.Fields["agents"].Items, "a try statement at byte zero is still a fallback")
}

func TestPythonCasePatternsBindTheirCaptures(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
LIMIT = "kept"

def handle(event):
    match event:
        case [TEXT, *rest]:
            return Agent(instructions=TEXT)
        case Point(x=LIMIT) as other:
            return Agent(instructions=LIMIT, name=other)
        case Color.RED | {"key": value}:
            return Agent(instructions=value)
    return Agent(instructions=TEXT)

match value:
    case LIMIT:
        pass
plain = Agent(instructions=TEXT, name=LIMIT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	for _, id := range []string{"handle.Agent", "handle.Agent#2", "handle.Agent#3", "handle.Agent#4"} {
		require.NotNil(t, agents[id], id)
		assert.Equal(t, adapter.CapabilityReadOnly, agents[id].Fields["instructions"].Capability, "%s reads a capture of the function", id)
	}
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["plain"].Fields["name"].Capability, "a module case pattern rebinds the constant")
}

func TestPythonAliasesOfImportsFollowTheirPosition(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT
early = Agent(instructions=prompt)
prompt = TEXT
late = Agent(instructions=prompt)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Hello\"\n")},
		},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["early"].Fields["instructions"].Capability, "an alias of an import is not in scope before its assignment")
	assert.Equal(t, "Hello", agents["late"].Fields["instructions"].Value)
	assert.Equal(t, "src/values.py", agents["late"].Fields["instructions"].File)
}

func TestPythonTypedSplatParametersHideOuterNames(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

def spread(*TEXT: str):
    return Agent(instructions=TEXT)

def keyed(**TEXT: int):
    return Agent(instructions=TEXT)

plain = Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["spread.Agent"].Fields["instructions"].Capability, "a typed star parameter hides the constant")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["keyed.Agent"].Fields["instructions"].Capability, "a typed double star parameter hides the constant")
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value)
}

func TestPythonReassignedResourcesGetOrdinalIDs(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
from langchain_core.prompts import PromptTemplate

agent = Agent(instructions="first")
agent = Agent(instructions="second")
prompt = PromptTemplate.from_template("one")
prompt = PromptTemplate.from_template("two")
single = Agent(instructions="only")
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 3)
	assert.Equal(t, "first", agents["agent#1"].Fields["instructions"].Value)
	assert.Equal(t, "second", agents["agent#2"].Fields["instructions"].Value)
	assert.True(t, adapter.IsPositionalID(agents["agent#1"]), "a reassigned name gives positional ids")
	assert.Equal(t, "only", agents["single"].Fields["instructions"].Value)
	assert.False(t, adapter.IsPositionalID(agents["single"]), "a name assigned once keeps its id")
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	assert.Equal(t, "one", prompts["prompt#1"].Fields["template"].Value)
	assert.Equal(t, "two", prompts["prompt#2"].Fields["template"].Value)
}

func TestPythonMutatedMessagesAreReadOnly(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from langchain_core.prompts import ChatPromptTemplate
from .values import MESSAGES
MESSAGES.append(("human", "more"))
chat = ChatPromptTemplate.from_messages(MESSAGES)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("MESSAGES = [(\"human\", \"Hi\")]\n")},
		},
	})
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1)
	messages := root.Fields["prompts"].Items[0].Fields["messages"]
	assert.Equal(t, adapter.CapabilityReadOnly, messages.Capability, "a mutated import is not a static message list")
	assert.Contains(t, messages.Reason, "mutated")
}

func TestPythonParameterDefaultsRunInTheEnclosingScope(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

def make(agent=Agent(instructions=TEXT)):
    pass

def early(agent=Agent(instructions=LATER)):
    pass

LATER = "late"

class Team:
    TEXT = "class"

    def build(self, agent=Agent(instructions=TEXT)):
        pass
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 3)
	assert.Equal(t, "module", agents["Agent"].Fields["instructions"].Value, "a default of a module function reads the module constant, in module scope")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["Agent#2"].Fields["instructions"].Capability, "a default runs with the definition, before a later constant")
	assert.Equal(t, "class", agents["Team.Agent"].Fields["instructions"].Value, "a default of a method reads the class attribute")
}

func TestPythonClientResetsDoNotDropTheClientKind(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI

class Assistant:
    def __init__(self):
        self.client = OpenAI()

    def ask(self):
        return self.client.chat.completions.create(model="gpt-5", messages=[{"role": "user", "content": "Hi"}])

    def close(self):
        self.client = None
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "a reset to None does not change the kind of the client")
}

func TestPythonWalrusBindingsNameResourcesAndClients(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
from openai import OpenAI

if (agent := Agent(instructions="walrus")):
    pass
if (client := OpenAI()):
    reply = client.chat.completions.create(model="gpt-5", messages=[{"role": "user", "content": "Hi"}])
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "a walrus names its resource once")
	assert.Equal(t, "agent", root.Fields["agents"].Items[0].Fields["id"].Value, "the walrus variable names the agent")
	require.Len(t, root.Fields["prompts"].Items, 1, "a walrus client is a client")
	assert.Equal(t, "reply", root.Fields["prompts"].Items[0].Fields["id"].Value)

	nested, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
team = [first := Agent(instructions="first"), Agent(instructions="second")]
`)}})
	require.NoError(t, err)
	agents := map[string]string{}
	for _, agent := range nested.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent.Fields["instructions"].Value.(string)
	}
	assert.Equal(t, map[string]string{"first": "first", "Agent": "second"}, agents, "a walrus inside a list names its call, and the list is not a chain")
}

func TestPythonLambdaDefaultsRunAtDefinition(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
early = lambda agent=Agent(instructions=LATER): agent
LATER = "late"
ready = lambda agent=Agent(instructions=TEXT): agent
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 2)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["Agent"].Fields["instructions"].Capability, "a lambda default runs with the definition, before a later constant")
	assert.Equal(t, "module", agents["Agent#2"].Fields["instructions"].Value)
}

func TestPythonClassNameMutationsFreezeTheClassAttribute(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
class Bot:
    items = ["a"]
    names = ["n"]
    kept = "same"

Bot.items.append("b")
Bot.names[0] = "x"
`)}})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["Bot.items"].Capability, "a mutating call through the class name freezes the attribute")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["Bot.names"].Capability, "a subscript write through the class name freezes the attribute")
	assert.Equal(t, adapter.CapabilityDirect, constants["Bot.kept"].Capability)
}

func TestPythonWalrusBindsALocalVariable(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

def build(items):
    if (TEXT := items[0]):
        pass
    return Agent(instructions=TEXT)

plain = Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["build.Agent"].Fields["instructions"].Capability, "the walrus binds a local that hides the module constant")
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityDirect, constants["TEXT"].Capability, "the walrus in the function does not mutate the module constant")
}

func TestPythonScopesResolveUsesAndMutationsAlike(t *testing.T) {
	bundle := adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT

LIST = ["one"]
MESSAGES = [("human", TEXT)]

def build():
    prompt = "local"
    LIST.append("two")
    return Agent(instructions=prompt)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte(`TEXT = "Hello"`)},
		},
	}
	root, err := parsePythonBundle(bundle)
	require.NoError(t, err)

	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "local", root.Fields["agents"].Items[0].Fields["instructions"].Value,
		"a use resolves the constant of its own function")
	var list, messages *adapter.Node
	for _, constant := range root.Fields["constants"].Items {
		switch constant.Fields["id"].Value {
		case "LIST":
			list = constant
		case "MESSAGES":
			messages = constant
		}
	}
	require.NotNil(t, list)
	assert.Equal(t, adapter.CapabilityReadOnly, list.Capability, "a mutation inside a function marks the module-level list")
	require.NotNil(t, messages)
	assert.Equal(t, adapter.CapabilityDirect, messages.Capability, "an imported value inside a constant resolves")
}

func TestPythonReturnedPromptCallsAreCollected(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from langchain_core.prompts import PromptTemplate

def build():
    return PromptTemplate.from_template("Hello {name}")
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1)
	prompt := root.Fields["prompts"].Items[0]
	assert.Equal(t, "build.PromptTemplate.from_template", prompt.Fields["id"].Value)
	assert.Equal(t, "Hello {{name}}", prompt.Fields["template"].Value)
}

func TestPythonPromptsOnSubscriptAndUnpackingTargetsAreCollected(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from langchain_core.prompts import PromptTemplate

PROMPTS = {}

def build():
    PROMPTS["greet"] = PromptTemplate.from_template("Hi {name}")
    first, second = PromptTemplate.from_template("One"), PromptTemplate.from_template("Two")
    PROMPTS["named"] = named = PromptTemplate.from_template("Named")
    return first, second
`)}})
	require.NoError(t, err)
	templates := map[string]string{}
	for _, prompt := range root.Fields["prompts"].Items {
		templates[prompt.Fields["id"].Value.(string)] = prompt.Fields["template"].Value.(string)
	}
	assert.Equal(t, map[string]string{
		"build.PromptTemplate.from_template":   "Hi {{name}}",
		"build.PromptTemplate.from_template#2": "One",
		"build.PromptTemplate.from_template#3": "Two",
		"build.named":                          "Named",
	}, templates, "a prompt without a variable name takes the name of the call, once per prompt")
	for _, prompt := range root.Fields["prompts"].Items {
		if id := prompt.Fields["id"].Value.(string); id != "build.named" {
			assert.True(t, adapter.IsPositionalID(prompt), "%s is positional", id)
		}
	}
}

func TestPythonMutationOfACollectionLeavesTheImportedValueWritable(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT

LIST = [TEXT]
LIST.append("more")
support = Agent(instructions=TEXT)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte(`TEXT = "Hello"`)},
		},
	})
	require.NoError(t, err)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, adapter.CapabilityDirect, instructions.Capability, "the mutated list does not mark the import")
	assert.NotNil(t, instructions.Encode)
}

func TestPythonConstantsUseEarlierLocalConstants(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
SYSTEM = "You help."
MESSAGES = [("system", SYSTEM), ("human", "Hi")]
LATER = [FUTURE]
FUTURE = "late"
`)}})
	require.NoError(t, err)
	byID := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		byID[constant.Fields["id"].Value.(string)] = constant
	}
	messages := code.MessageList(byID["MESSAGES"])
	require.NotNil(t, messages)
	assert.Equal(t, adapter.CapabilityDirect, messages.Capability, "a local system prompt resolves")
	assert.Equal(t, adapter.CapabilityReadOnly, byID["LATER"].Capability, "a constant declared later is not in scope")
}

func TestPythonMessagesResolveTheScopedConstant(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from langchain_core.prompts import ChatPromptTemplate

MESSAGES = [("human", "module")]

def build():
    MESSAGES = [("human", "local")]
    return ChatPromptTemplate.from_messages(MESSAGES)

def early():
    chat = ChatPromptTemplate.from_messages(MESSAGES_LATER)
    MESSAGES_LATER = [("human", "late")]
    return chat
`)}})
	require.NoError(t, err)
	byID := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		byID[prompt.Fields["id"].Value.(string)] = prompt
	}
	local := byID["build.ChatPromptTemplate.from_messages"]
	require.NotNil(t, local)
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "local"}}, local.Fields["messages"].Value)
	early := byID["early.chat"]
	require.NotNil(t, early)
	assert.Equal(t, adapter.CapabilityReadOnly, early.Fields["messages"].Capability, "a name declared after the call is not in scope")
}

func TestPythonAllowlistMatchesOnlyTheExactModule(t *testing.T) {
	assert.NotNil(t, pythonAllowedImports("agents"))
	assert.Nil(t, pythonAllowedImports("agents.prompts"), "a repository package with a framework prefix is followed as an import")
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(
			"from agents import Agent\nfrom agents.prompts import TEXT\nsupport = Agent(instructions=TEXT)\n",
		)},
		Dependencies: map[string]adapter.Document{
			// An absolute module resolves from the repository root.
			"agents/prompts.py": {Path: "agents/prompts.py", Data: []byte(`TEXT = "Hello"`)},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "Hello", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestPythonParenthesizedAndConcatenatedStringsAreStatic(t *testing.T) {
	document := adapter.Document{Path: "main.py", Data: []byte("TEXT = (\"one \"\n    'two')\nSOLO = (\"solo\")\nMIXED = (\"a\" f\"{b}\")\n")}
	tree, err := code.ParseTree(document, sitter.NewLanguage(pythonlanguage.Language()))
	require.NoError(t, err)
	defer tree.Close()

	joined := code.FindNode(tree.RootNode(), document.Data, "parenthesized_expression", "(\"one \"\n    'two')")
	require.NotNil(t, joined)
	value := pythonValue(joined, document.Data, nil)
	assert.Equal(t, "one two", value.Value)
	assert.Equal(t, adapter.CapabilityDirect, value.Capability)
	encoded, err := value.Encode("next")
	require.NoError(t, err)
	assert.Equal(t, `"next"`, string(encoded), "the first quote style wins")
	assert.Equal(t, len("(\"one \"\n    'two')")-2, value.End-value.Start, "the edit replaces every part")

	solo := code.FindNode(tree.RootNode(), document.Data, "parenthesized_expression", `("solo")`)
	require.NotNil(t, solo)
	assert.Equal(t, "solo", pythonValue(solo, document.Data, nil).Value)
	mixed := code.FindNode(tree.RootNode(), document.Data, "concatenated_string", "\"a\" f\"{b}\"")
	require.NotNil(t, mixed)
	assert.Equal(t, adapter.CapabilityReadOnly, pythonValue(mixed, document.Data, nil).Capability)
}

func TestPythonLocalAssignmentShadowsAnImport(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(
			"from agents import Agent\nfrom .values import TEXT\nTEXT = \"local\"\nsupport = Agent(instructions=TEXT)\n",
		)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte(`TEXT = "imported"`)},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "local", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestPythonImportsReadTheSyntaxTree(t *testing.T) {
	document := adapter.Document{Path: "main.py", Data: []byte(
		"from agents import Agent as Bot, function_tool  # tools\nfrom .values import (TEXT as text,\n    OTHER)\n",
	)}
	tree, err := code.ParseTree(document, sitter.NewLanguage(pythonlanguage.Language()))
	require.NoError(t, err)
	defer tree.Close()

	last := tree.RootNode().NamedChild(tree.RootNode().NamedChildCount() - 1)
	assert.Equal(t, map[string]string{"Bot": "openai-agent", "function_tool": "tool-decorator"}, pythonImports(tree.RootNode(), document.Data).at(last, document.Data))
	assert.Equal(t, []pythonImportBinding{
		{module: ".values", name: "TEXT", alias: "text"},
		{module: ".values", name: "OTHER", alias: "OTHER"},
	}, pythonImportBindings(tree.RootNode(), document.Data))
}

func TestNoneIsAConstant(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte("NOTHING = None\n")}})
	require.NoError(t, err)
	require.Len(t, root.Fields["constants"].Items, 1)
	assert.Equal(t, "NOTHING", root.Fields["constants"].Items[0].Fields["id"].Value)
}

func TestPythonNestedConstructorsAreResources(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from crewai import Agent, Task
agents = [Agent(role="writer", goal="Write"), Agent(role="editor", goal="Edit")]
task = Task(description="Draft", agent=Agent(role="helper", goal="Help"))
`)}})
	require.NoError(t, err)
	ids := []string{}
	for _, agent := range root.Fields["agents"].Items {
		ids = append(ids, agent.Fields["id"].Value.(string))
	}
	assert.Len(t, ids, 3, "each nested constructor is a resource")
	assert.Len(t, root.Fields["prompts"].Items, 1, "the assigned task is named once")
}

func TestPythonStringsKeepTheLineEndingsOfTheFile(t *testing.T) {
	document := adapter.Document{Path: "main.py", Data: []byte("TEXT = \"\"\"first\r\nsecond\"\"\"\r\n")}
	tree, err := code.ParseTree(document, sitter.NewLanguage(pythonlanguage.Language()))
	require.NoError(t, err)
	defer tree.Close()

	literal := code.FindNode(tree.RootNode(), document.Data, "string", "\"\"\"first\r\nsecond\"\"\"")
	require.NotNil(t, literal)
	value := pythonValue(literal, document.Data, nil)
	assert.Equal(t, "first\nsecond", value.Value, "the value has one line ending")
	encoded, err := value.Encode("one\ntwo\nthree")
	require.NoError(t, err)
	assert.Equal(t, "\"\"\"one\r\ntwo\r\nthree\"\"\"", string(encoded), "the write keeps the line ending of the file")
}

func TestPythonInstancesResolveByScope(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI
from anthropic import Anthropic

client = OpenAI()
first = client.chat.completions.create(model="gpt", messages=[{"role": "user", "content": "one"}])

def other():
    client = Anthropic()
    return client.messages.create(model="claude", messages=[{"role": "user", "content": "two"}])

shared = OpenAI()
shared = Anthropic()
third = shared.messages.create(model="claude", messages=[{"role": "user", "content": "three"}])
`)}})
	require.NoError(t, err)
	ids := map[string]bool{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids[prompt.Fields["id"].Value.(string)] = true
	}
	assert.True(t, ids["first"], "the module-level client is an OpenAI client")
	assert.True(t, ids["other.client.messages.create"], "the local client is an Anthropic client")
	assert.False(t, ids["third"], "a name that two frameworks assign is ambiguous")
}

func TestPythonOpenAIAgentInstructionsArePlainText(t *testing.T) {
	for name, text := range map[string]string{
		"placeholder-like": "Return {answer} only.",
		"JSON example":     `Reply as JSON: {"answer": "yes"}`,
		"double braces":    "Keep {{literal}} braces.",
	} {
		t.Run(name, func(t *testing.T) {
			source := "from agents import Agent\nsupport = Agent(instructions=" + string(code.EncodePythonString(text, `"`)) + ")\n"
			root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(source)}})
			require.NoError(t, err)
			require.Len(t, root.Fields["agents"].Items, 1)
			instructions := root.Fields["agents"].Items[0].Fields["instructions"]

			assert.Equal(t, text, instructions.Value, "the Agents SDK sends instructions without formatting them")
			require.Equal(t, adapter.CapabilityDirect, instructions.Capability, instructions.Reason)
			encoded, err := instructions.Encode(text + " Be brief.")
			require.NoError(t, err)
			assert.Equal(t, string(code.EncodePythonString(text+" Be brief.", `"`)), string(encoded))
		})
	}
}

func TestPythonCrewAIAgentTextTranslatesPlaceholders(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from crewai import Agent

researcher = Agent(role="Researcher", goal="Research {topic}", backstory="Expert in {topic}.")
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	goal := root.Fields["agents"].Items[0].Fields["goal"]
	assert.Equal(t, "Research {{topic}}", goal.Value, "CrewAI fills {topic} from kickoff inputs")
	encoded, err := goal.Encode("Study {{topic}}")
	require.NoError(t, err)
	assert.Equal(t, `"Study {topic}"`, string(encoded))
}

func TestPythonHTMLInPromptsRoundTrips(t *testing.T) {
	for name, html := range map[string]string{
		"tags":          "<p>Answer in <b>bold</b>.</p>",
		"attributes":    `<a href="https://example.com" class='link'>docs</a>`,
		"entities":      "Use &lt;tags&gt; &amp; &quot;quotes&quot;.",
		"multiline":     "<ul>\n  <li>One</li>\n  <li>Two</li>\n</ul>",
		"inline style":  `<p style="color: red">Warn</p>`,
		"style block":   "<style>p { color: red; }</style><p>Warn</p>",
		"script block":  "<script>if (a) { b(); }</script>",
		"template vars": "<p>Hello {name}</p>",
	} {
		t.Run(name, func(t *testing.T) {
			for _, quote := range []string{`"`, `"""`} {
				source := "from agents import Agent\nsupport = Agent(instructions=" + string(code.EncodePythonString(html, quote)) + ")\n"
				root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(source)}})
				require.NoError(t, err)
				require.Len(t, root.Fields["agents"].Items, 1)
				instructions := root.Fields["agents"].Items[0].Fields["instructions"]

				assert.Equal(t, html, instructions.Value, "quote %s", quote)
				require.Equal(t, adapter.CapabilityDirect, instructions.Capability, "quote %s: %s", quote, instructions.Reason)
				encoded, err := instructions.Encode(html + "<br>")
				require.NoError(t, err)
				decoded, _, err := code.DecodePythonString(encoded)
				require.NoError(t, err)
				assert.Equal(t, html+"<br>", decoded, "quote %s", quote)
			}
		})
	}
}

func TestPythonModuleQualifiedFrameworkCallsAreFound(t *testing.T) {
	for name, source := range map[string]string{
		"import module":    "import agents\nsupport = agents.Agent(name=\"Support\", instructions=\"Help.\")\n",
		"import alias":     "import agents as oa\nsupport = oa.Agent(name=\"Support\", instructions=\"Help.\")\n",
		"openai qualified": "import openai\nclient = openai.OpenAI()\nreply = client.chat.completions.create(model=\"gpt-5\", messages=[{\"role\": \"system\", \"content\": \"Help.\"}])\n",
	} {
		t.Run(name, func(t *testing.T) {
			root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(source)}})
			require.NoError(t, err)
			found := len(root.Fields["agents"].Items) + len(root.Fields["prompts"].Items)
			assert.Equal(t, 1, found, "the framework call is found")
		})
	}
}

func TestPythonDynamicLocalShadowsAnImport(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(
			"from agents import Agent\nfrom .values import TEXT\nTEXT = load()\nsupport = Agent(instructions=TEXT)\n",
		)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte(`TEXT = "imported"`)},
		},
	})
	require.NoError(t, err)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, adapter.CapabilityReadOnly, instructions.Capability, "the dynamic local hides the import")
	assert.Nil(t, instructions.Value)
}

func TestPythonUnnamedNestedConstructorsGetDistinctIDs(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from pydantic_ai import Agent
from crewai import Agent as Crew
team = [Agent("model-a"), Agent("model-b")]
crew = [Crew(role="writer"), Crew(role="editor")]
`)}})
	require.NoError(t, err)
	ids := []string{}
	for _, agent := range root.Fields["agents"].Items {
		ids = append(ids, agent.Fields["id"].Value.(string))
	}
	assert.ElementsMatch(t, []string{"Agent", "Agent#2", "writer", "editor"}, ids)
}

func TestPythonAttributeAssignmentsKeepFrameworkResources(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

class Service:
    def __init__(self):
        self.agent = Agent(name="support", instructions="Help")
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, "support", root.Fields["agents"].Items[0].Fields["id"].Value)
}

func TestPythonKeywordOnlyPromptFactoriesHaveFields(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from langchain_core.prompts import PromptTemplate, ChatPromptTemplate
single = PromptTemplate.from_template(template="Hello {name}")
chat = ChatPromptTemplate.from_messages(messages=[("human", "Hi")])
`)}})
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	assert.Equal(t, "Hello {{name}}", prompts["single"].Fields["template"].Value)
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "Hi"}}, prompts["chat"].Fields["messages"].Value)
}

func TestPythonNumbersWithSignsSeparatorsAndBases(t *testing.T) {
	document := adapter.Document{Path: "main.py", Data: []byte("A = -1\nB = 1_000\nC = 0x10\nD = 0o17\nE = 0b101\nF = -2.5\nG = -x\n")}
	tree, err := code.ParseTree(document, sitter.NewLanguage(pythonlanguage.Language()))
	require.NoError(t, err)
	defer tree.Close()
	for text, expected := range map[string]float64{"-1": -1, "1_000": 1000, "0x10": 16, "0o17": 15, "0b101": 5, "-2.5": -2.5} {
		kind := "integer"
		if strings.HasPrefix(text, "-") {
			kind = "unary_operator"
		}
		node := code.FindNode(tree.RootNode(), document.Data, kind, text)
		require.NotNil(t, node, text)
		value := pythonValue(node, document.Data, nil)
		assert.Equal(t, expected, value.Value, text)
		assert.Equal(t, adapter.CapabilityDirect, value.Capability, text)
	}
	dynamic := code.FindNode(tree.RootNode(), document.Data, "unary_operator", "-x")
	require.NotNil(t, dynamic)
	assert.Equal(t, adapter.CapabilityReadOnly, pythonValue(dynamic, document.Data, nil).Capability)
}

func TestPythonIntegersBeyondExactFloatsAreReadOnly(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

EXACT = 9007199254740992
BIG = 9007199254740993
support = Agent(instructions="Help", exact=EXACT, big=9007199254740993, huge=99999999999999999999, negative=-9007199254740993, floating=1e20)
`)}})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, float64(1<<53), constants["EXACT"].Value, "the largest exact integer is a constant")
	assert.Nil(t, constants["BIG"], "an integer without an exact float64 is not a literal constant")
	require.Len(t, root.Fields["agents"].Items, 1)
	fields := root.Fields["agents"].Items[0].Fields
	assert.Equal(t, float64(1<<53), fields["exact"].Value)
	for _, field := range []string{"big", "huge", "negative"} {
		assert.Equal(t, adapter.CapabilityReadOnly, fields[field].Capability, "%s has no exact float64", field)
		assert.Contains(t, fields[field].Reason, "number is not representable", field)
	}
	assert.Equal(t, 1e20, fields["floating"].Value, "a float literal keeps its float value")
}

func TestPythonConstantAliasChainsResolve(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
BASE = "You help."
ALIAS = BASE
MESSAGES = [("system", ALIAS)]
`)}})
	require.NoError(t, err)
	for _, constant := range root.Fields["constants"].Items {
		if constant.Fields["id"].Value == "MESSAGES" {
			messages := code.MessageList(constant)
			require.NotNil(t, messages)
			assert.Equal(t, adapter.CapabilityDirect, messages.Capability, "a two-hop alias resolves")
		}
	}
}

func TestPythonListedPromptsAndModuleQualifiedFactories(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
import langchain_core.prompts as p
from langchain_core.prompts import PromptTemplate
prompts = [PromptTemplate.from_template("one"), PromptTemplate.from_template("two")]
chat = p.ChatPromptTemplate.from_messages([("human", "Hi")])
`)}})
	require.NoError(t, err)
	ids := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids[prompt.Fields["id"].Value.(string)] = prompt
	}
	assert.Equal(t, "one", ids["prompts#1"].Fields["template"].Value)
	assert.Equal(t, "two", ids["prompts#2"].Fields["template"].Value)
	require.NotNil(t, ids["chat"], "a module-qualified factory is a prompt")
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "Hi"}}, ids["chat"].Fields["messages"].Value)
}

func TestPythonQualifiedIDsIncludeEveryEnclosingScope(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI
from anthropic import Anthropic

class First:
    def setup(self):
        client = OpenAI()
        prompt = "first"
        return client.chat.completions.create(model="gpt", messages=[{"role": "user", "content": prompt}])

class Second:
    def setup(self):
        client = Anthropic()
        prompt = "second"
        return client.messages.create(model="claude", messages=[{"role": "user", "content": prompt}])

def outer():
    SHARED = "outer"
    def inner():
        return SHARED
`)}})
	require.NoError(t, err)
	ids := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids[prompt.Fields["id"].Value.(string)] = prompt
	}
	require.NotNil(t, ids["First.setup.client.chat.completions.create"], "the class is part of the id")
	require.NotNil(t, ids["Second.setup.client.messages.create"], "each class keeps its own client kind")
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	require.NotNil(t, constants["First.setup.prompt"])
	require.NotNil(t, constants["Second.setup.prompt"])
	require.NotNil(t, constants["outer.SHARED"])
}

func TestPythonChainedAssignmentsAndTaskOutputs(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
from crewai import Task
SYSTEM = DEFAULT = "You help."
support = Agent(instructions=SYSTEM)
task = Task(description="Write about {topic}", expected_output="A post on {topic}")
`)}})
	require.NoError(t, err)
	assert.Equal(t, "You help.", root.Fields["agents"].Items[0].Fields["instructions"].Value, "a chained assignment is static")
	require.Len(t, root.Fields["prompts"].Items, 1)
	task := root.Fields["prompts"].Items[0]
	assert.Equal(t, "A post on {{topic}}", task.Fields["expected_output"].Value, "the task output translates its placeholders")
	encoded, err := task.Fields["expected_output"].Encode("An essay on {{topic}}")
	require.NoError(t, err)
	assert.Equal(t, `"An essay on {topic}"`, string(encoded))
}

func TestPlaceholderResidueBesideAVariableIsReadOnly(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from langchain_core.prompts import PromptTemplate
escaped = PromptTemplate.from_template("Use }} before {name}")
plain = PromptTemplate.from_template("Hello {name}")
`)}})
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	assert.Equal(t, adapter.CapabilityReadOnly, prompts["escaped"].Fields["template"].Capability, "a stray brace before the placeholder is ambiguous")
	assert.Equal(t, "Hello {{name}}", prompts["plain"].Fields["template"].Value)
}

func TestPythonDecoratedIDsIncludeEveryEnclosingScope(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from mcp.server.fastmcp import FastMCP
mcp = FastMCP("server")

class First:
    @mcp.tool()
    def run(self, value: str):
        """First."""
        return value

class Second:
    @mcp.tool()
    def run(self, value: str):
        """Second."""
        return value

    @mcp.prompt()
    def greet(self, name: str):
        """Greet."""
        return name
`)}})
	require.NoError(t, err)
	tools := map[string]*adapter.Node{}
	for _, tool := range root.Fields["tools"].Items {
		tools[tool.Fields["id"].Value.(string)] = tool
	}
	require.Len(t, tools, 2, "two decorated methods with one name keep distinct ids")
	assert.Equal(t, "First.", tools["First.run"].Fields["description"].Value)
	assert.Equal(t, "Second.", tools["Second.run"].Fields["description"].Value)
	require.Len(t, root.Fields["prompts"].Items, 1)
	assert.Equal(t, "Second.greet", root.Fields["prompts"].Items[0].Fields["id"].Value)
}

func TestPythonAttributeClientsAreRequests(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI
from anthropic import Anthropic

class Assistant:
    def __init__(self):
        self.client = OpenAI()

    def ask(self):
        return self.client.chat.completions.create(model="gpt", messages=[{"role": "user", "content": "one"}])

class Other:
    def __init__(self):
        self.client = Anthropic()

    def ask(self):
        return self.client.messages.create(model="claude", messages=[{"role": "user", "content": "two"}])
`)}})
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	require.Len(t, prompts, 2, "a client stored on an attribute keeps its kind")
	require.Contains(t, prompts, "Assistant.ask.self.client.chat.completions.create")
	assert.Equal(t, "gpt", prompts["Assistant.ask.self.client.chat.completions.create"].Fields["model"].Value)
	require.Contains(t, prompts, "Other.ask.self.client.messages.create")
	assert.Equal(t, "claude", prompts["Other.ask.self.client.messages.create"].Fields["model"].Value)
}

func TestPythonMutatedImportIsReadOnly(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT, OTHER
TEXT += " now"
support = Agent(instructions=TEXT)
other = Agent(instructions=OTHER)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Help\"\nOTHER = \"Keep\"\n")},
		},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	mutated := agents["support"].Fields["instructions"]
	assert.Equal(t, adapter.CapabilityReadOnly, mutated.Capability, "an import that this file mutates is read-only")
	assert.Equal(t, "mutated constants are read-only", mutated.Reason)
	assert.Equal(t, adapter.CapabilityDirect, agents["other"].Fields["instructions"].Capability)
}

func TestPythonStarImportIsFollowed(t *testing.T) {
	source := []byte(`
from agents import Agent
from .values import *
support = Agent(instructions=TEXT)
`)
	dependencies, err := pythonAdapter{}.Dependencies(adapter.Document{Path: "src/main.py", Data: source})
	require.NoError(t, err)
	assert.Contains(t, dependencies, "src/values.py", "a star import names its module as a dependency")

	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary:      adapter.Document{Path: "src/main.py", Data: source},
		Dependencies: map[string]adapter.Document{"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Help\"\n")}},
	})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, adapter.CapabilityReadOnly, instructions.Capability)
	assert.Contains(t, instructions.Reason, "star import cannot resolve TEXT statically")
}

func TestPythonChainedAttributeAssignmentsNameTheVariable(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

class Bot:
    def __init__(self):
        self.agent = local = Agent(instructions="Local")
        agent = self.backup = Agent(instructions="Backup")
        self.only = Agent(instructions="Attribute")
`)}})
	require.NoError(t, err)
	agents := map[string]string{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent.Fields["instructions"].Value.(string)
	}
	assert.Equal(t, map[string]string{
		"Bot.__init__.local": "Local",
		"Bot.__init__.agent": "Backup",
		"Bot.__init__.Agent": "Attribute",
	}, agents, "a chain names its call once, after its variable, and a chain of attributes names it like an unassigned call")
}

func TestPythonLambdasSeeLaterImportsAndConstants(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
build = lambda: Agent(instructions=TEXT)
early = Agent(instructions=TEXT)
from agents import Agent
TEXT = "Later"
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "the lambda body runs after the module loads, and the immediate call does not see the import")
	assert.Equal(t, "Later", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestPythonLoopWithAndExceptTargetsHideConstants(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
LIMIT = "kept"

def loop(items):
    for TEXT in items:
        pass
    return Agent(instructions=TEXT)

def context():
    with open("f") as TEXT:
        return Agent(instructions=TEXT)

def catch():
    try:
        pass
    except ValueError as TEXT:
        return Agent(instructions=TEXT)

def unpack(pairs):
    for key, TEXT in pairs:
        pass
    return Agent(instructions=TEXT)

names = [TEXT for TEXT in ["a"]]
plain = Agent(instructions=TEXT, name=LIMIT)
for LIMIT in ["b"]:
    pass
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	for _, id := range []string{"loop.Agent", "context.Agent", "catch.Agent", "unpack.Agent"} {
		require.NotNil(t, agents[id], id)
		assert.Equal(t, adapter.CapabilityReadOnly, agents[id].Fields["instructions"].Capability, "%s reads the local binding", id)
	}
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value, "a comprehension target binds its name in the comprehension only")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["plain"].Fields["name"].Capability, "a module loop rebinds the constant")
}

func TestPythonAttributeAssignedPromptIsCollected(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from langchain_core.prompts import PromptTemplate

class Assistant:
    def __init__(self):
        self.template = PromptTemplate.from_template("Hi {name}")
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "a prompt on an attribute is collected once")
	prompt := root.Fields["prompts"].Items[0]
	assert.Equal(t, "Assistant.__init__.self.template", prompt.Fields["id"].Value)
	assert.Equal(t, "Hi {{name}}", prompt.Fields["template"].Value)
}

func TestPythonSplatArgumentsFillNoPositionalField(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
support = Agent(name="support", instructions="Help", **options)
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	agent := root.Fields["agents"].Items[0]
	assert.Equal(t, "support", agent.Fields["name"].Value, "a splat does not replace the keyword name")
	assert.Equal(t, "Help", agent.Fields["instructions"].Value)
	assert.Equal(t, adapter.CapabilityDirect, agent.Fields["name"].Capability)
}

func TestPythonMethodsDoNotSeeClassAttributes(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
SYSTEM = "module"

class Bot:
    SYSTEM = "class"
    direct = Agent(instructions=SYSTEM)

    def run(self):
        return Agent(instructions=SYSTEM)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "class", agents["Bot.direct"].Fields["instructions"].Value, "the class body sees its own attribute")
	assert.Equal(t, "module", agents["Bot.run.Agent"].Fields["instructions"].Value, "a method sees the module name, not the class attribute")
}

func TestPythonReassignedConstantsAreReadOnlyWithDistinctIDs(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
NAME = "one"
NAME = "two"
support = Agent(instructions=NAME)
`)}})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	require.Contains(t, constants, "NAME")
	require.Contains(t, constants, "NAME#2", "each assignment keeps an id of its own")
	for id, constant := range constants {
		assert.Equal(t, adapter.CapabilityReadOnly, constant.Capability, id)
		assert.Equal(t, "reassigned constants are read-only", constant.Reason, id)
	}
	assert.NotEqual(t, adapter.CapabilityDirect, root.Fields["agents"].Items[0].Fields["instructions"].Capability)
}

func TestPythonStandaloneFastMCPImportIsFollowed(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from fastmcp import FastMCP
mcp = FastMCP("server")

@mcp.tool()
def run(value: str):
    """Run."""
    return value

@mcp.prompt()
def greet(name: str):
    """Greet."""
    return name
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1, "the standalone fastmcp package is a FastMCP server")
	assert.Equal(t, "run", root.Fields["tools"].Items[0].Fields["id"].Value)
	require.Len(t, root.Fields["prompts"].Items, 1)
	assert.Equal(t, "greet", root.Fields["prompts"].Items[0].Fields["id"].Value)
}

func TestPythonTuplesAreWrittenAsTuples(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
PAIR = ("a", "b")
ONE = ("a",)
LIST = ["a"]
`)}})
	require.NoError(t, err)
	encode := func(id string, value []any) string {
		node, err := root.Resolve(`constants[id="` + id + `"]`)
		require.NoError(t, err)
		encoded, err := node.Encode(value)
		require.NoError(t, err)
		return string(encoded)
	}
	assert.Equal(t, `("x", "y")`, encode("PAIR", []any{"x", "y"}), "a tuple stays a tuple")
	assert.Equal(t, `("z",)`, encode("ONE", []any{"z"}), "a tuple of one item keeps its comma")
	assert.Equal(t, `["z"]`, encode("LIST", []any{"z"}))
}

func TestPythonFrameworkStarImportBindsItsSymbols(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import *
support = Agent(instructions="Help")
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1, "a star import of a framework binds its symbols")
	assert.Equal(t, "Help", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}

func TestPythonClassAttributeClientsResolveThroughSelf(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI

class Assistant:
    client = OpenAI()

    def ask(self):
        return self.client.chat.completions.create(model="gpt", messages=[{"role": "user", "content": "one"}])
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "self.client names the class attribute client")
	assert.Equal(t, "Assistant.ask.self.client.chat.completions.create", root.Fields["prompts"].Items[0].Fields["id"].Value)
}

func TestPythonDocstringAfterACommentIsTheDescription(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from langchain_core.tools import tool

@tool
def search(query: str):
    # The docstring follows this comment.
    """Search the docs."""
    return query
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["tools"].Items, 1)
	assert.Equal(t, "Search the docs.", root.Fields["tools"].Items[0].Fields["description"].Value)
}

func TestPythonImportStaysVisibleBeforeALaterShadowingAssignment(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT
first = Agent(instructions=TEXT)
TEXT = "Local"
second = Agent(instructions=TEXT)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Imported\"\n")},
		},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Imported", agents["first"].Fields["instructions"].Value, "a use before the assignment sees the import")
	assert.Equal(t, adapter.CapabilityDirect, agents["first"].Fields["instructions"].Capability)
	assert.Equal(t, "Local", agents["second"].Fields["instructions"].Value, "a use after the assignment sees the local value")
}

func TestPythonPositionalArgumentsFollowTheFrameworkSignatures(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
from strands import Agent as StrandsAgent
support = Agent("support", "You are helpful.")
strands = StrandsAgent("claude-model", system_prompt="Follow the policy.")
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "support", agents["support"].Fields["name"].Value)
	assert.Equal(t, "You are helpful.", agents["support"].Fields["instructions"].Value, "the Agents SDK takes instructions second")
	assert.Equal(t, "claude-model", agents["strands"].Fields["model"].Value, "Strands takes the model first")
	assert.Equal(t, "Follow the policy.", agents["strands"].Fields["system_prompt"].Value, "a positional model does not replace the keyword prompt")
}

func TestPythonFrameworkAliasesFollowTheirImportPosition(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from crewai import Agent
crew = Agent(role="Writer", goal="Write", backstory="Years of {topic}")

def build():
    from pydantic_ai import Agent
    return Agent("gpt-5")

from agents import Agent
support = Agent(name="support", instructions="Help")
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 3)
	assert.Equal(t, "Years of {{topic}}", agents["crew"].Fields["backstory"].Value, "the first import is the CrewAI agent at that point")
	assert.Equal(t, "gpt-5", agents["build.Agent"].Fields["model"].Value, "a local import applies inside its function")
	assert.Equal(t, "Help", agents["support"].Fields["instructions"].Value, "the later import is the Agents SDK agent")
	assert.Nil(t, agents["support"].Fields["backstory"])
}

func TestPythonDottedMutationsFreezeTheRootConstant(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
SETTINGS = {"values": {"a": 1}}
SETTINGS["values"].update({"b": 2})

class Bot:
    items = ["a"]

    def add(self):
        self.items.append("b")
`)}})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["SETTINGS"].Capability, "a mutating call through a subscript freezes the constant")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["Bot.items"].Capability, "a mutating call through self freezes the class attribute")
}

func TestPythonSelfAttributeWritesFreezeTheClassAttribute(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
class Bot:
    items = ["a"]
    names = ["n"]
    greeting = "Hi"
    kept = "same"

    def add(self):
        self.items[0] = "b"

    def more(self):
        self.names += ["m"]

    def reset(self):
        self.greeting = "Hello"
`)}})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["Bot.items"].Capability, "a subscript write through self freezes the class attribute")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["Bot.names"].Capability, "an augmented assignment through self freezes the class attribute")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["Bot.greeting"].Capability, "an assignment through self freezes the class attribute")
	assert.Equal(t, adapter.CapabilityDirect, constants["Bot.kept"].Capability, "an attribute that no method writes stays direct")
	assert.Nil(t, constants["self"], "self is not a constant")
}

func TestPythonClassBodiesSeeOnlyEarlierImports(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from crewai import Agent

class Team:
    writer = Agent(role="Writer", goal="Write", backstory="Years of {topic}")

    def run(self):
        return Agent(name="runner", instructions="Run")

from agents import Agent
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Years of {{topic}}", agents["Team.writer"].Fields["backstory"].Value, "a class body runs where it is and sees the CrewAI import")
	assert.Equal(t, "Run", agents["runner"].Fields["instructions"].Value, "a method runs later and sees the Agents SDK import")
}

func TestPythonParenthesizedValuesIgnoreComments(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
PROMPT = (  # the greeting
    "Help the customer."
)
`)}})
	require.NoError(t, err)
	prompt, err := root.Resolve(`constants[id="PROMPT"]`)
	require.NoError(t, err)
	assert.Equal(t, "Help the customer.", prompt.Value)
	assert.Equal(t, adapter.CapabilityDirect, prompt.Capability)
}

func TestPythonAwaitedCallsKeepTheirAssignmentName(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI
client = OpenAI()

async def ask():
    reply = await client.chat.completions.create(model="gpt", messages=[{"role": "user", "content": "Hi"}])
    other = (client.chat.completions.create(model="gpt", messages=[{"role": "user", "content": "Yo"}]))
    return reply, other
`)}})
	require.NoError(t, err)
	ids := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids = append(ids, prompt.Fields["id"].Value.(string))
	}
	assert.ElementsMatch(t, []string{"ask.reply", "ask.other"}, ids, "await and parentheses keep the variable name")
}

func TestPythonPositionalMessagesAreAMessageList(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from strands import Agent
agent = Agent("claude-model", [{"role": "user", "content": "Hi"}], system_prompt="Follow the policy.")
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	messages := root.Fields["agents"].Items[0].Fields["messages"]
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "Hi"}}, messages.Value, "a positional messages list is read as the keyword one")
}

func TestPythonImportedValuesFollowTheScopeOfTheirImport(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT

def build():
    from .other import TEXT
    return Agent(instructions=TEXT)

support = Agent(instructions=TEXT)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Outer\"\n")},
			"src/other.py":  {Path: "src/other.py", Data: []byte("TEXT = \"Inner\"\n")},
		},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Inner", agents["build.Agent"].Fields["instructions"].Value, "a local import binds its name inside the function")
	assert.Equal(t, "Outer", agents["support"].Fields["instructions"].Value, "module-level code keeps the module-level import")
}

func TestPythonImportsStayVisibleBeforeAShadowingAssignmentInAnyScope(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT

class Team:
    first = Agent(instructions=TEXT)
    TEXT = "class"
    second = Agent(instructions=TEXT)

def build():
    from .values import TEXT
    a = Agent(instructions=TEXT)
    TEXT = "local"
    b = Agent(instructions=TEXT)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Imported\"\n")},
		},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Imported", agents["Team.first"].Fields["instructions"].Value, "a class body sees the module import until it binds the name")
	assert.Equal(t, "class", agents["Team.second"].Fields["instructions"].Value)
	assert.Equal(t, "Imported", agents["build.a"].Fields["instructions"].Value, "a local import is visible before the local assignment")
	assert.Equal(t, "local", agents["build.b"].Fields["instructions"].Value)
}

func TestPythonMutatedLocalImportIsReadOnly(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent

def build():
    from .values import TEXT
    TEXT += "!"
    return Agent(instructions=TEXT)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"Help\"\n")},
		},
	})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, adapter.CapabilityReadOnly, instructions.Capability, "a mutation of a function-local import freezes it")
	assert.Equal(t, "mutated constants are read-only", instructions.Reason)
}

func TestPythonReassignedAndMutatedConstantsStayPositional(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
NAME = "one"
NAME = "two"
NAME += "!"
`)}})
	require.NoError(t, err)
	first, err := root.Resolve(`constants[id="NAME"]`)
	require.NoError(t, err)
	assert.Equal(t, adapter.CapabilityReadOnly, first.Capability)
	assert.True(t, adapter.IsPositionalID(first), "the read-only mark does not replace the positional id")
}

func TestPythonParenthesizedMessagesAndConstructors(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI
client = (OpenAI())
reply = client.chat.completions.create(model="gpt", messages=([{"role": "user", "content": "Hi"}]))
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "a parenthesized constructor is an instance")
	messages := root.Fields["prompts"].Items[0].Fields["messages"]
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "Hi"}}, messages.Value, "a parenthesized messages list unwraps")
}

func TestPythonChainedAssignmentsBindEveryNameAndCollectOnce(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
from openai import OpenAI
first = second = Agent(instructions="Help")
primary = backup = OpenAI()
one = primary.chat.completions.create(model="gpt", messages=[{"role": "user", "content": "A"}])
two = backup.chat.completions.create(model="gpt", messages=[{"role": "user", "content": "B"}])
`)}})
	require.NoError(t, err)
	agents := []string{}
	for _, agent := range root.Fields["agents"].Items {
		agents = append(agents, agent.Fields["id"].Value.(string))
	}
	assert.Equal(t, []string{"first"}, agents, "a chained constructor is one resource, named by the outer name")
	prompts := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts = append(prompts, prompt.Fields["id"].Value.(string))
	}
	assert.ElementsMatch(t, []string{"one", "two"}, prompts, "every name of a chained client binding is a client")
}

func TestPythonWithAsBindsAClient(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI

def ask():
    with OpenAI() as client:
        reply = client.chat.completions.create(model="gpt", messages=[{"role": "user", "content": "Hi"}])
        return reply
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["prompts"].Items, 1, "a with-as binding is a client")
	assert.Equal(t, "ask.reply", root.Fields["prompts"].Items[0].Fields["id"].Value)
}

func TestPythonFunctionsSeeLaterModuleConstants(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

def build():
    return Agent(instructions=SYSTEM)

early = Agent(instructions=SYSTEM)
SYSTEM = "Help"
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "Help", agents["build.Agent"].Fields["instructions"].Value, "a function body runs after the module loads")
	assert.NotEqual(t, adapter.CapabilityDirect, agents["early"].Fields["instructions"].Capability, "module-level code before the constant does not see it")
}

func TestPythonCommonListMutatorsFreezeConstants(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
ITEMS = ["b", "a"]
ITEMS.sort()
NAMES = ["x"]
NAMES.remove("x")
KEPT = ["k"]
`)}})
	require.NoError(t, err)
	capability := func(id string) adapter.Capability {
		node, err := root.Resolve(`constants[id="` + id + `"]`)
		require.NoError(t, err)
		return node.Capability
	}
	assert.Equal(t, adapter.CapabilityReadOnly, capability("ITEMS"), "sort mutates in place")
	assert.Equal(t, adapter.CapabilityReadOnly, capability("NAMES"), "remove mutates in place")
	assert.Equal(t, adapter.CapabilityDirect, capability("KEPT"))
}

func TestPythonParametersHideModuleConstants(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
SYSTEM = "Module"

def build(SYSTEM, other="x"):
    return Agent(instructions=SYSTEM)

def plain():
    return Agent(instructions=SYSTEM)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.NotEqual(t, adapter.CapabilityDirect, agents["build.Agent"].Fields["instructions"].Capability, "a parameter hides the module constant")
	assert.Equal(t, "Module", agents["plain.Agent"].Fields["instructions"].Value)
}

func TestPythonChainedPromptOrdinalsArePositional(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from langchain_core.prompts import PromptTemplate
prompts = PromptTemplate.from_template("one") | PromptTemplate.from_template("two")
`)}})
	require.NoError(t, err)
	first, err := root.Resolve(`prompts[id="prompts#1"]`)
	require.NoError(t, err)
	assert.True(t, adapter.IsPositionalID(first), "an ordinal in a chain is a position")
}

func TestPythonParametersHideNamesBeforeTheBodyReadsThem(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent
TEXT = "Module"

def build(TEXT):
    local = TEXT
    return Agent(instructions=local)

handler = lambda TEXT: Agent(instructions=TEXT)
support = Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.NotEqual(t, adapter.CapabilityDirect, agents["build.Agent"].Fields["instructions"].Capability, "a local alias of a parameter is dynamic")
	assert.NotEqual(t, adapter.CapabilityDirect, agents["Agent"].Fields["instructions"].Capability, "a lambda parameter is dynamic")
	assert.Equal(t, "Module", agents["support"].Fields["instructions"].Value, "a lambda parameter does not hide the module constant outside the lambda")
}

func TestPythonLambdaParametersBindOnlyInsideTheLambda(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

def build():
    pick = lambda TEXT: TEXT
    inner = lambda TEXT: Agent(instructions=TEXT)
    outer = Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 2)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["build.Agent"].Fields["instructions"].Capability, "the lambda body reads its parameter")
	assert.Equal(t, "module", agents["build.outer"].Fields["instructions"].Value, "a lambda parameter does not hide the constant in the enclosing function")
}

func TestPythonExceptAliasesHideTheOuterConstant(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

error = "module"

def handle():
    try:
        pass
    except (ValueError, KeyError) as error:
        inside = Agent(instructions=error)
    after = Agent(instructions=error)

try:
    pass
except Exception as error:
    pass
module_level = Agent(instructions=error)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 3)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["handle.inside"].Fields["instructions"].Capability)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["handle.after"].Fields["instructions"].Capability, "the alias binds the name in the function")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["module_level"].Fields["instructions"].Capability, "the alias rebinds the module constant")
}

func TestPythonMutationThroughAnAliasFreezesEveryName(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from .values import ITEMS
from .values import ITEMS as ALSO
from .values import TEXT
ITEMS.append("b")
WRAPPED = [ALSO]
LOCAL = ["a"]
COPY = LOCAL
COPY.append("b")
PLAIN = [TEXT]
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("ITEMS = [\"a\"]\nTEXT = \"Hello\"\n")},
		},
	})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["WRAPPED"].Capability, "the second import of a mutated value is read-only")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["LOCAL"].Capability, "a mutation through a local alias freezes the origin")
	assert.Equal(t, adapter.CapabilityDirect, constants["PLAIN"].Capability, "an untouched import stays writable")
}

func TestPythonDeleteStatementsFreezeTheCollection(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
ITEMS = ["a", "b"]
CONFIG = {"key": "value", "other": "kept"}
PAIR = ["x", "y"]
NAME = "gone"
KEPT = ["z"]
del ITEMS[0]
del CONFIG["key"], PAIR[1]
del NAME
`)}})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	for _, id := range []string{"ITEMS", "CONFIG", "PAIR", "NAME"} {
		assert.Equal(t, adapter.CapabilityReadOnly, constants[id].Capability, id)
	}
	assert.Equal(t, adapter.CapabilityDirect, constants["KEPT"].Capability)
}

func TestPythonGlobalAndNonlocalAssignmentsRebindTheOuterConstant(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
KEPT = "kept"

def rebind():
    global TEXT
    TEXT = "changed"

def outer():
    INNER = "outer"
    def inner():
        nonlocal INNER
        INNER = "inner"
        return Agent(instructions=INNER)
    return Agent(instructions=INNER)

def local():
    KEPT = "local"
    return Agent(instructions=KEPT)

module_level = Agent(instructions=TEXT)
kept = Agent(instructions=KEPT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["module_level"].Fields["instructions"].Capability, "a global assignment rebinds the module constant")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["TEXT"].Capability)
	assert.Equal(t, "reassigned constants are read-only", constants["TEXT"].Reason)
	assert.Nil(t, constants["rebind.TEXT"], "the global assignment is not a local of the function")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["outer.Agent"].Fields["instructions"].Capability, "a nonlocal assignment rebinds the constant of the enclosing function")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["outer.inner.Agent"].Fields["instructions"].Capability)
	assert.Equal(t, "kept", agents["kept"].Fields["instructions"].Value, "a plain local assignment leaves the module constant")
	assert.Equal(t, "local", agents["local.Agent"].Fields["instructions"].Value)
}

func TestPythonWalrusPromptsKeepTheirOwnName(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI

client = OpenAI()
batch = [reply := client.chat.completions.create(model="gpt-5", messages=[{"role": "user", "content": "Hi"}])]
chain = alias = client.chat.completions.create(model="gpt-5", messages=[{"role": "user", "content": "Chain"}])
`)}})
	require.NoError(t, err)
	ids := []string{}
	for _, prompt := range root.Fields["prompts"].Items {
		ids = append(ids, prompt.Fields["id"].Value.(string))
	}
	assert.ElementsMatch(t, []string{"reply", "chain"}, ids, "a walrus inside a list names its prompt, and a chain keeps the outer name")
}

func TestPythonMessagesSkipInnerLocalBindings(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from openai import OpenAI

client = OpenAI()
MESSAGES = [{"role": "user", "content": "module"}]
handlers = [lambda MESSAGES: client.chat.completions.create(model="gpt-5", messages=MESSAGES)]
replies = [client.chat.completions.create(model="gpt-5", messages=MESSAGES) for MESSAGES in batches]
plain = client.chat.completions.create(model="gpt-5", messages=MESSAGES)
`)}})
	require.NoError(t, err)
	prompts := map[string]*adapter.Node{}
	for _, prompt := range root.Fields["prompts"].Items {
		prompts[prompt.Fields["id"].Value.(string)] = prompt
	}
	require.Len(t, prompts, 3)
	// A request inside a lambda or a comprehension has no variable name, so
	// the callable names it.
	assert.Equal(t, adapter.CapabilityReadOnly, prompts["client.chat.completions.create"].Fields["messages"].Capability, "a lambda parameter is not the module messages")
	assert.Equal(t, adapter.CapabilityReadOnly, prompts["client.chat.completions.create#2"].Fields["messages"].Capability, "a comprehension target is not the module messages")
	assert.Equal(t, []syncdomain.Message{{Role: syncdomain.RoleUser, Content: "module"}}, prompts["plain"].Fields["messages"].Value)
}

func TestPythonLambdaDefaultsAndFirstIterablesSeeTheOuterName(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
ITEMS = ["a"]
defaulted = lambda TEXT=Agent(instructions=TEXT): TEXT
body = lambda TEXT: Agent(instructions=TEXT)
first = [Agent(instructions=TEXT) for TEXT in Agent(instructions=TEXT).items]
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 4)
	assert.Equal(t, "module", agents["Agent"].Fields["instructions"].Value, "a lambda default runs in the enclosing scope")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["Agent#2"].Fields["instructions"].Capability, "the lambda body reads its parameter")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["Agent#3"].Fields["instructions"].Capability, "the comprehension body reads its target")
	assert.Equal(t, "module", agents["Agent#4"].Fields["instructions"].Value, "the first iterable runs in the enclosing scope")
}

func TestPythonImportsOfReassignedAndMutatedNamesAreReadOnlyBindings(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import TEXT, ITEMS, PLAIN

support = Agent(instructions=TEXT)
WRAPPED = [ITEMS]
other = Agent(instructions=PLAIN)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"first\"\nTEXT = \"second\"\nITEMS = [\"a\"]\nITEMS.append(\"b\")\nPLAIN = \"plain\"\n")},
		},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	instructions := agents["support"].Fields["instructions"]
	assert.Equal(t, "first", instructions.Value, "the import binds the first literal")
	assert.Equal(t, adapter.CapabilityReadOnly, instructions.Capability)
	assert.Equal(t, "reassigned constants are read-only", instructions.Reason)
	assert.Empty(t, instructions.Code, "a reassigned name is not an unresolved import")
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["WRAPPED"].Capability, "a mutated import stays a read-only binding")
	assert.Equal(t, "plain", agents["other"].Fields["instructions"].Value)
}

func TestPythonNonlocalBindsTheNearestFunctionThatHasTheName(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

def outer():
    TEXT = "outer"
    def middle():
        def inner():
            nonlocal TEXT
            TEXT = "inner"
        return Agent(instructions=TEXT)
    return Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 2)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["outer.Agent"].Fields["instructions"].Capability, "the nonlocal skips the function without the name and rebinds the outer constant")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["outer.middle.Agent"].Fields["instructions"].Capability)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Nil(t, constants["outer.middle.TEXT"], "the function without the name gets no binding")
	assert.NotNil(t, constants["outer.TEXT#2"], "the outer constant is reassigned")
}

func TestPythonDefsAndClassesBindTheirNames(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
NAME = "module"

def TEXT():
    pass

class NAME:
    pass

rebound = Agent(instructions=TEXT)
classed = Agent(instructions=NAME)

def outer():
    def LABEL():
        pass
    def middle():
        def inner():
            nonlocal LABEL
            LABEL = "inner"
        return Agent(instructions=LABEL)
    return middle
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 3)
	assert.Equal(t, adapter.CapabilityReadOnly, agents["rebound"].Fields["instructions"].Capability, "a def rebinds the constant")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["classed"].Fields["instructions"].Capability, "a class rebinds the constant")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["outer.middle.Agent"].Fields["instructions"].Capability, "the nonlocal binds in the function whose def owns the name")
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["TEXT"].Capability)
	assert.Nil(t, constants["outer.middle.LABEL"], "the function between gets no binding")
}

func TestPythonStarImportDoesNotSupplyFunctionLocals(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import *

def build(TEXT, items):
    LOCAL = items[0]
    first = Agent(instructions=TEXT)
    second = Agent(instructions=LOCAL)
    third = Agent(instructions=OTHER)
`)},
		Dependencies: map[string]adapter.Document{"src/values.py": {Path: "src/values.py", Data: []byte("OTHER = \"Help\"\n")}},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	require.Len(t, agents, 3)
	for _, id := range []string{"build.first", "build.second"} {
		instructions := agents[id].Fields["instructions"]
		assert.Equal(t, adapter.CapabilityReadOnly, instructions.Capability, id)
		assert.Equal(t, "dynamic expression has no safe inverse", instructions.Reason, "a local is dynamic, not a name of the star import")
		assert.Empty(t, instructions.Code, id)
	}
	assert.Contains(t, agents["build.third"].Fields["instructions"].Reason, "star import cannot resolve OTHER statically")
}

func TestPythonNonlocalBindsAnImportedEnclosingName(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

def outer():
    from .values import TEXT
    def inner():
        nonlocal TEXT
        TEXT = "inner"
    return Agent(instructions=TEXT)

plain = Agent(instructions=TEXT)
`)},
		Dependencies: map[string]adapter.Document{"src/values.py": {Path: "src/values.py", Data: []byte("TEXT = \"imported\"\n")}},
	})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["outer.Agent"].Fields["instructions"].Capability, "the nonlocal rebinds the imported name of the enclosing function")
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value, "the module constant stays")
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityDirect, constants["TEXT"].Capability, "the module constant is not reassigned")
}

func TestPythonGlobalStatementsScopeReadsAndMutations(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

ITEMS = ["module"]
TEXT = "module"

def outer():
    ITEMS = ["outer"]
    TEXT = "outer"
    def inner():
        global ITEMS, TEXT
        ITEMS.append("x")
        return Agent(instructions=TEXT)
    return Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "module", agents["outer.inner.Agent"].Fields["instructions"].Value, "a global read skips the local of the enclosing function")
	assert.Equal(t, "outer", agents["outer.Agent"].Fields["instructions"].Value)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["ITEMS"].Capability, "a global mutation freezes the module constant")
	assert.Equal(t, adapter.CapabilityDirect, constants["outer.ITEMS"].Capability, "the local of the enclosing function is untouched")
}

func TestPythonImportedAliasesShareMutations(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from .values import ITEMS, ALSO, OTHER
ITEMS.append("b")
WRAPPED = [ALSO]
PLAIN = [OTHER]
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("ITEMS = [\"a\"]\nALSO = ITEMS\nOTHER = [\"c\"]\n")},
		},
	})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["WRAPPED"].Capability, "an alias of the mutated import binds the same object")
	assert.Equal(t, adapter.CapabilityDirect, constants["PLAIN"].Capability)
}

func TestPythonNonlocalSkipsAGlobalOfAFunctionBetween(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

def outer():
    TEXT = "outer"
    def middle():
        global TEXT
        def inner():
            nonlocal TEXT
            TEXT = "inner"
        return inner
    return Agent(instructions=TEXT)

plain = Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["outer.Agent"].Fields["instructions"].Capability, "the nonlocal rebinds the local of outer, past the global of middle")
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value, "the global of middle does not move the nonlocal write to the module")
}

func TestPythonAugmentedAssignmentsBindInTheirFunction(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

def outer():
    def middle(TEXT):
        TEXT += " more"
        def inner():
            nonlocal TEXT
            TEXT = "inner"
        return inner
    return Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "module", agents["outer.Agent"].Fields["instructions"].Value, "the nonlocal lands on middle, which binds the name, so outer reads the module constant")
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityDirect, constants["TEXT"].Capability, "the module constant is not rebound")
}

func TestPythonImportOfAnAliasOfAMutatedNameIsReadOnly(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{
		Primary: adapter.Document{Path: "src/main.py", Data: []byte(`
from agents import Agent
from .values import ALSO, TEXT_ALSO

WRAPPED = [ALSO]
support = Agent(instructions=TEXT_ALSO)
`)},
		Dependencies: map[string]adapter.Document{
			"src/values.py": {Path: "src/values.py", Data: []byte("ITEMS = [\"a\"]\nALSO = ITEMS\nITEMS.append(\"b\")\nTEXT = \"first\"\nTEXT_ALSO = TEXT\nTEXT = \"second\"\n")},
		},
	})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityReadOnly, constants["WRAPPED"].Capability, "the alias of a mutated name imports the read-only origin")
	instructions := root.Fields["agents"].Items[0].Fields["instructions"]
	assert.Equal(t, "first", instructions.Value, "the alias of a reassigned name imports its first literal")
	assert.Equal(t, adapter.CapabilityReadOnly, instructions.Capability)
	assert.Empty(t, instructions.Code, "the alias is not an unresolved import")
}

func TestPythonBareAnnotationsBindTheirName(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
OTHER = "module"

def build():
    TEXT: str
    return Agent(instructions=TEXT)

def assigned():
    OTHER: str
    OTHER = "local"
    return Agent(instructions=OTHER)

class Bot:
    TEXT: str
    annotated = Agent(instructions=TEXT)

TEXT: str
plain = Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["build.Agent"].Fields["instructions"].Capability, "a bare annotation makes the name local")
	assert.Equal(t, "local", agents["assigned.Agent"].Fields["instructions"].Value, "an annotation with a later assignment is one binding")
	assert.Equal(t, "module", agents["Bot.annotated"].Fields["instructions"].Value, "a bare annotation in a class body binds nothing")
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value, "a bare annotation at module level binds nothing")
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityDirect, constants["assigned.OTHER"].Capability, "the annotation does not count as a reassignment")
}

func TestPythonExceptGroupAliasesBindTheirName(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

error = "module"

def handle():
    try:
        pass
    except* ExceptionGroup as error:
        pass
    return Agent(instructions=error)
`)}})
	require.NoError(t, err)
	require.Len(t, root.Fields["agents"].Items, 1)
	assert.Equal(t, adapter.CapabilityReadOnly, root.Fields["agents"].Items[0].Fields["instructions"].Capability, "an except* alias binds the name in the function")
}

func TestPythonAugmentedAndDeleteWritesInAFunctionBindLocals(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"
ITEMS = ["a"]
COUNT = ["b"]

def build():
    TEXT += " more"
    del ITEMS
    return Agent(instructions=TEXT)

def shared():
    global COUNT
    del COUNT

plain = Agent(instructions=TEXT)
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, adapter.CapabilityReadOnly, agents["build.Agent"].Fields["instructions"].Capability, "the augmented write makes the name a local")
	assert.Equal(t, "module", agents["plain"].Fields["instructions"].Value)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityDirect, constants["TEXT"].Capability, "a local augmented write does not freeze the module constant")
	assert.Equal(t, adapter.CapabilityDirect, constants["ITEMS"].Capability, "a local delete does not freeze the module constant")
	assert.Equal(t, adapter.CapabilityReadOnly, constants["COUNT"].Capability, "a delete under a global statement mutates the module constant")
}

func TestPythonClassBodiesSeeTheModuleConstantBeforeTheClassBindsIt(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

TEXT = "module"

class Bot:
    early = Agent(instructions=TEXT)
    TEXT = "class"
    late = Agent(instructions=TEXT)

class Twice:
    early = Agent(instructions=TEXT)
    TEXT = "one"
    TEXT = "two"
    late = Agent(instructions=TEXT)

class Dynamic:
    early = Agent(instructions=TEXT)
    TEXT = compute()
`)}})
	require.NoError(t, err)
	agents := map[string]*adapter.Node{}
	for _, agent := range root.Fields["agents"].Items {
		agents[agent.Fields["id"].Value.(string)] = agent
	}
	assert.Equal(t, "module", agents["Bot.early"].Fields["instructions"].Value, "the class body sees the module constant until it binds the name")
	assert.Equal(t, "class", agents["Bot.late"].Fields["instructions"].Value)
	assert.Equal(t, "module", agents["Twice.early"].Fields["instructions"].Value, "a reassigned class attribute still leaves the earlier use to the module")
	assert.Equal(t, adapter.CapabilityReadOnly, agents["Twice.late"].Fields["instructions"].Capability)
	assert.Equal(t, "module", agents["Dynamic.early"].Fields["instructions"].Value, "a dynamic class attribute still leaves the earlier use to the module")
}

func TestPythonReassignedAliasesDoNotSpreadMutations(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
ITEMS = ["a"]
ALSO = ITEMS
ALSO = ["b"]
ALSO.append("c")
WRAPPED = [ITEMS]
`)}})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, adapter.CapabilityDirect, constants["ITEMS"].Capability, "a name that is assigned again is no alias of its first value")
	assert.Equal(t, adapter.CapabilityDirect, constants["WRAPPED"].Capability)
}

func TestPythonWalrusValuesReadAsConstants(t *testing.T) {
	root, err := parsePythonBundle(adapter.SourceBundle{Primary: adapter.Document{Path: "main.py", Data: []byte(`
from agents import Agent

NAME = (OTHER := "text")
ITEMS = [first := "hello", "world"]
support = Agent(instructions=NAME)
`)}})
	require.NoError(t, err)
	constants := map[string]*adapter.Node{}
	for _, constant := range root.Fields["constants"].Items {
		constants[constant.Fields["id"].Value.(string)] = constant
	}
	assert.Equal(t, "text", constants["NAME"].Value, "the value of a walrus is the value of the assignment")
	assert.Equal(t, adapter.CapabilityDirect, constants["NAME"].Capability)
	assert.Equal(t, "text", constants["OTHER"].Value)
	assert.Equal(t, adapter.CapabilityDirect, constants["ITEMS"].Capability, "a walrus inside a list is a literal item")
	assert.Equal(t, []any{"hello", "world"}, constants["ITEMS"].Value)
	assert.Equal(t, "text", root.Fields["agents"].Items[0].Fields["instructions"].Value)
}
