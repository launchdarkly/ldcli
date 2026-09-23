package adapters

// Adapter converts between an external prompt format and the common prompt
// representation used by reference syncing.
type Adapter interface {
	Parse([]byte) (Prompt, error)
	Render(Prompt) ([]byte, error)
}

// Prompt is the format-independent data an adapter may extract.
type Prompt struct {
	Mode     Mode
	Key      string
	Name     string
	Messages []Message
}

// Mode identifies how the parsed prompt is represented in LaunchDarkly.
type Mode string

const (
	ModeAgent      Mode = "agent"
	ModeCompletion Mode = "completion"
)

// Valid reports whether the mode is supported by prompt sync.
func (mode Mode) Valid() bool {
	return mode == ModeAgent || mode == ModeCompletion
}

// Message is one role/content pair in a prompt.
type Message struct {
	Role    Role
	Content string
}

// Role identifies the speaker for a prompt message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Valid reports whether the role can be represented by LaunchDarkly.
func (role Role) Valid() bool {
	return role == RoleSystem || role == RoleUser || role == RoleAssistant
}
