package evaluate

import (
	"fmt"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_multiContext(t *testing.T) {
	stdin := io.NopCloser(strings.NewReader(`{"kind": "user", "key": "user-123"}`))

	ldCtx := newContextFlag(stdin)

	fields := []struct {
		val    string
		setter pflag.Value
	}{
		{val: "@-", setter: ldCtx.jsonValue()},
		{val: "key=mobile", setter: ldCtx.magicValue()},
		{val: "kind=device", setter: ldCtx.magicValue()},
		{val: "os=android", setter: ldCtx.magicValue()},
		{val: "key=something", setter: ldCtx.magicValue()},
		{val: "kind=location", setter: ldCtx.magicValue()},
	}

	for _, field := range fields {
		err := field.setter.Set(field.val)
		if err != nil {
			t.Fatalf("parseFields error: %v", err)
		}
	}

	res, err := ldCtx.ldContext()
	assert.NoError(t, err)
	assert.Equal(t, `{"kind":"multi","device":{"key":"mobile","os":"android"},"location":{"key":"something"},"user":{"key":"user-123"}}`, res.JSONString())
	assert.Equal(t, ldCtx.changed, true)

	err = ldCtx.magicValue().Set("key=new-key")
	require.NoError(t, err)
	_, err = ldCtx.ldContext()
	require.EqualError(t, err, "multi-context cannot have same kind more than once")
}

func Test_subFlagTypes(t *testing.T) {
	stdin := io.NopCloser(strings.NewReader("pasted contents"))

	ldCtx := newContextFlag(stdin)

	fields := []struct {
		val    string
		setter pflag.Value
	}{
		{val: "key=somebot", setter: ldCtx.rawValue()},
		{val: "robot=Hubot", setter: ldCtx.rawValue()},
		{val: "victories=123", setter: ldCtx.magicValue()},
		{val: `{"key": "someone", "kind": "device", "device": "somedevice"}`, setter: ldCtx.jsonValue()},
	}

	for _, field := range fields {
		err := field.setter.Set(field.val)
		if err != nil {
			t.Fatalf("parseFields error: %v", err)
		}
	}

	context, err := ldCtx.ldContext()
	require.NoError(t, err)
	assert.JSONEq(t, `{
  "kind": "multi",
  "device": {
    "key": "someone",
    "device": "somedevice"
  },
  "user": {
    "key": "somebot",
    "robot": "Hubot",
    "victories": 123
  }
}`, context.JSONString())
}

func Test_specialFields(t *testing.T) {
	stdin := io.NopCloser(strings.NewReader("pasted contents"))

	ldCtx := newContextFlag(stdin)

	fields := []struct {
		val    string
		setter pflag.Value
	}{
		{val: "key=some-flag-key", setter: ldCtx.rawValue()},
		{val: "kind=device", setter: ldCtx.magicValue()},
		{val: `ids=[123, 321]`, setter: ldCtx.magicValue()},
		{val: `nest[key]=test`, setter: ldCtx.magicValue()},
		{val: `nested[kind]=test`, setter: ldCtx.magicValue()},
	}

	for _, field := range fields {
		err := field.setter.Set(field.val)
		if err != nil {
			t.Fatalf("parseFields error: %v", err)
		}
	}

	assert.Equal(t, ldCtx.changed, true)
	context, err := ldCtx.ldContext()
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"device","key":"some-flag-key","ids":[123,321],"nest":{"key":"test"},"nested":{"kind":"test"}}`, context.JSONString())
}

func Test_parseFields(t *testing.T) {
	stdin := io.NopCloser(strings.NewReader("pasted contents"))
	ldCtx := newContextFlag(stdin)
	fields := []struct {
		val    string
		setter pflag.Value
	}{
		{val: `key=testing`, setter: ldCtx.magicValue()},
		{val: "robot=Hubot", setter: ldCtx.rawValue()},
		{val: "destroyer=false", setter: ldCtx.rawValue()},
		{val: "helper=true", setter: ldCtx.rawValue()},
		{val: "location=@work", setter: ldCtx.rawValue()},
		{val: "input=@-", setter: ldCtx.magicValue()},
		{val: "enabled=true", setter: ldCtx.magicValue()},
		{val: "victories=123", setter: ldCtx.magicValue()},
		{val: `ids=[123, 321]`, setter: ldCtx.magicValue()},
		{val: `user={"id": 1}`, setter: ldCtx.magicValue()},
		{val: `user[name]=myname`, setter: ldCtx.magicValue()},
	}

	for _, field := range fields {
		err := field.setter.Set(field.val)
		if err != nil {
			t.Fatalf("parseFields error: %v", err)
		}
	}

	context, err := ldCtx.ldContext()
	require.NoError(t, err)
	assert.JSONEq(t, `{
  "kind": "user",
  "key": "testing",
  "robot": "Hubot",
  "ids": [
    123,
    321
  ],
  "enabled": true,
  "victories": 123,
  "destroyer": "false",
  "helper": "true",
  "location": "@work",
  "input": "pasted contents",
  "user": {
    "id": 1,
    "name": "myname"
  }
}`, context.JSONString())
}

func Test_parseFields_nested(t *testing.T) {
	stdin := io.NopCloser(strings.NewReader("pasted contents"))
	ldCtx := newContextFlag(stdin)

	fields := []struct {
		val    string
		setter pflag.Value
	}{
		{val: "key=testing", setter: ldCtx.rawValue()},
		{val: "branch[name]=patch-1", setter: ldCtx.rawValue()},
		{val: "robots[]=Hubot", setter: ldCtx.rawValue()},
		{val: "robots[]=Dependabot", setter: ldCtx.rawValue()},
		{val: "labels[][name]=bug", setter: ldCtx.rawValue()},
		{val: "labels[][color]=red", setter: ldCtx.rawValue()},
		{val: "labels[][colorOptions][]=red", setter: ldCtx.rawValue()},
		{val: "labels[][colorOptions][]=blue", setter: ldCtx.rawValue()},
		{val: "labels[][name]=feature", setter: ldCtx.rawValue()},
		{val: "labels[][color]=green", setter: ldCtx.rawValue()},
		{val: "labels[][colorOptions][]=red", setter: ldCtx.rawValue()},
		{val: "labels[][colorOptions][]=green", setter: ldCtx.rawValue()},
		{val: "labels[][colorOptions][]=yellow", setter: ldCtx.rawValue()},
		{val: "nested[][key1][key2][key3]=value", setter: ldCtx.rawValue()},
		{val: "empty[]", setter: ldCtx.rawValue()},
		{val: "branch[protections]=true", setter: ldCtx.magicValue()},
		{val: "ids[]=123", setter: ldCtx.magicValue()},
		{val: "ids[]=456", setter: ldCtx.magicValue()},
	}

	for _, field := range fields {
		err := field.setter.Set(field.val)
		if err != nil {
			t.Fatalf("parseFields error: %v", err)
		}
	}

	context, err := ldCtx.ldContext()
	require.NoError(t, err)
	assert.JSONEq(t, `{
  "kind": "user",
  "key": "testing",
  "robots": [
    "Hubot",
    "Dependabot"
  ],
  "labels": [
    {
      "name": "bug",
      "color": "red",
      "colorOptions": [
        "red",
        "blue"
      ]
    },
    {
      "color": "green",
      "colorOptions": [
        "red",
        "green",
        "yellow"
      ],
      "name": "feature"
    }
  ],
  "nested": [
    {
      "key1": {
        "key2": {
          "key3": "value"
        }
      }
    }
  ],
  "empty": [],
  "ids": [
    123,
    456
  ],
  "branch": {
    "name": "patch-1",
    "protections": true
  }
}
`, context.JSONString())
}

func Test_parseFields_errors(t *testing.T) {
	tests := []struct {
		name     string
		val1     string
		val2     string
		expected string
	}{
		{
			name:     "cannot overwrite string to array",
			val1:     "object[field]=A",
			val2:     "object[field][]=this should be an error",
			expected: `expected array type under "field", got string`,
		},
		{
			name:     "cannot overwrite string to object",
			val1:     "object[field]=B",
			val2:     "object[field][field2]=this should be an error",
			expected: `expected map type under "field", got string`,
		},
		{
			name:     "cannot overwrite object to string",
			val1:     "object[field][field2]=C",
			val2:     "object[field]=this should be an error",
			expected: `unexpected override existing field under "field"`,
		},
		{
			name:     "cannot overwrite object to array",
			val1:     "object[field][field2]=D",
			val2:     "object[field][]=this should be an error",
			expected: `expected array type under "field", got map[string]interface {}`,
		},
		{
			name:     "cannot overwrite array to string",
			val1:     "object[field][]=E",
			val2:     "object[field]=this should be an error",
			expected: `unexpected override existing field under "field"`,
		},
		{
			name:     "cannot overwrite array to object",
			val1:     "object[field][]=F",
			val2:     "object[field][field2]=this should be an error",
			expected: `expected map type under "field", got []interface {}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdin := io.NopCloser(strings.NewReader("pasted contents"))
			ldCtx := newContextFlag(stdin)
			err := ldCtx.rawValue().Set(tt.val1)
			require.NoError(t, err)
			err = ldCtx.magicValue().Set(tt.val2)
			require.EqualError(t, err, tt.expected)
		})
	}
}

func Test_magicFieldValue(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "ldcli-test")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	f2, err := os.CreateTemp(t.TempDir(), "ldcli-test.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	f3, err := os.CreateTemp(t.TempDir(), "ldcli-test.magic")
	if err != nil {
		t.Fatal(err)
	}
	defer f3.Close()

	fmt.Fprint(f, "file contents")
	fmt.Fprint(f2, `{"key": "val"}`)
	fmt.Fprint(f3, `true`)

	stdin := io.NopCloser(strings.NewReader("pasted contents"))

	tests := []struct {
		name    string
		v       string
		want    interface{}
		wantErr bool
	}{
		{
			name:    "string",
			v:       "hello",
			want:    "hello",
			wantErr: false,
		},
		{
			name:    "bool true",
			v:       "true",
			want:    true,
			wantErr: false,
		},
		{
			name:    "bool false",
			v:       "false",
			want:    false,
			wantErr: false,
		},
		{
			name:    "int",
			v:       "123",
			want:    int(123),
			wantErr: false,
		},
		{
			name:    "float",
			v:       "123.0",
			want:    123.0,
			wantErr: false,
		},
		{
			name:    "null",
			v:       "null",
			want:    nil,
			wantErr: false,
		},
		{
			name:    "stdin",
			v:       "@-",
			want:    "pasted contents",
			wantErr: false,
		},
		{
			name:    "file",
			v:       "@" + f.Name(),
			want:    "file contents",
			wantErr: false,
		},
		{
			name:    "file.json",
			v:       "@" + f2.Name(),
			want:    map[string]interface{}{"key": "val"},
			wantErr: false,
		},
		{
			name:    "file.magic",
			v:       "@" + f3.Name(),
			want:    true,
			wantErr: false,
		},
		{
			name:    "file error",
			v:       "@",
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := magicFieldValue(tt.v, stdin)
			if (err != nil) != tt.wantErr {
				t.Errorf("magicFieldValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
