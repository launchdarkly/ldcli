package evaluate

import (
	"fmt"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"
	"github.com/spf13/pflag"
	"io"
)

const (
	contextKey = "key"
)

type contextFlagType int

const (
	typeJSON contextFlagType = iota
	typeRaw
	typeMagic
)

type ldContextFlag struct {
	currentContextData *map[string]interface{}

	builder ldcontext.MultiBuilder

	changed bool
	in      io.ReadCloser
}

func newContextFlag(in io.ReadCloser) *ldContextFlag {
	ssv := &ldContextFlag{
		currentContextData: &map[string]interface{}{},
		in:                 in,
	}
	return ssv
}

func (c *ldContextFlag) AddContext(context ldcontext.Context) {
	c.changed = true
	c.builder.Add(context)
}

// setKey finalises the current data defined for the context, and creates a new
// one ready to be used for a new context.
func (c *ldContextFlag) setKey(value interface{}) error {
	if err := c.finaliseCurrentContextData(); err != nil {
		return err
	}

	c.currentContextData = &map[string]interface{}{
		contextKey: value,
	}

	return nil
}

func (c *ldContextFlag) finaliseCurrentContextData() error {
	if c.currentContextData != nil && len(*c.currentContextData) > 0 {
		b := ldcontext.NewBuilder("")
		for k, v := range *c.currentContextData {
			if !b.TrySetValue(k, ldvalue.CopyArbitraryValue(v)) {
				return fmt.Errorf("failed to set context value: k=%s; v=%s", k, v)
			}
		}

		ldctx, err := b.TryBuild()
		if err != nil {
			return fmt.Errorf("failed to create context: %w; given context: %+v", err, c.currentContextData)
		}

		c.AddContext(ldctx)
	}

	return nil
}

func (c *ldContextFlag) ldContext() (ldcontext.Context, error) {
	if err := c.finaliseCurrentContextData(); err != nil {
		return ldcontext.Context{}, err
	}

	return c.builder.TryBuild()
}

// Flag type conversion methods
func (c *ldContextFlag) jsonValue() pflag.Value {
	return &contextFlagWrapper{realContext: c, contextType: typeJSON}
}

func (c *ldContextFlag) magicValue() pflag.Value {
	return &contextFlagWrapper{realContext: c, contextType: typeMagic}
}

func (c *ldContextFlag) rawValue() pflag.Value {
	return &contextFlagWrapper{realContext: c, contextType: typeRaw}
}

// contextFlagWrapper handles type specific fields, and assigns the value to the real context
// This is necessary so we can use a pflag.FlagSet.Var and assign the value of multiple flags
// to the same variable. The reason we have multiple flags is to allow us to have different
// kinds of input, e.g. key value pairs where the user can decide whether to send a raw value
// or have it as a parsed value (i.e. int/bool etc), as well as plain JSON.
type contextFlagWrapper struct {
	realContext *ldContextFlag
	contextType contextFlagType
}

func (s *contextFlagWrapper) Set(val string) error {
	switch s.contextType {
	case typeMagic:
		return s.realContext.parseFields(val, true)
	case typeRaw:
		return s.realContext.parseFields(val, false)
	case typeJSON:
		ldctx, err := jsonFieldValue(val, s.realContext.in)
		if err != nil {
			return fmt.Errorf("failed to create context from JSON: %w", err)
		}

		s.realContext.AddContext(ldctx)
		return nil
	}

	return fmt.Errorf("invalid context type: %v", s.contextType)
}
func (s *contextFlagWrapper) Type() string   { return "ldContextFlag" }
func (s *contextFlagWrapper) String() string { return "todo" }
