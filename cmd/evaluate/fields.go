// This file parses several different ways of specifying key=value pairs.
// This taken from the GitHub cli tool, and adjusted for LaunchDarkly's needs.
// Original source: https://github.com/cli/cli/blob/trunk/pkg/cmd/api/fields.go
package evaluate

import (
	"encoding/json"
	"fmt"
	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
)

const (
	keyStart     = '['
	keyEnd       = ']'
	keySeparator = '='
)

func (c *ldContextFlag) parseFields(f string, isMagicField bool) error {
	var valueIndex int
	var keystack []string
	keyStartAt := 0
parseLoop:
	for i, r := range f {
		switch r {
		case keyStart:
			if keyStartAt == 0 {
				keystack = append(keystack, f[0:i])
			}
			keyStartAt = i + 1
		case keyEnd:
			keystack = append(keystack, f[keyStartAt:i])
		case keySeparator:
			if keyStartAt == 0 {
				keystack = append(keystack, f[0:i])
			}
			valueIndex = i + 1
			break parseLoop
		}
	}

	if len(keystack) == 0 {
		return fmt.Errorf("invalid key: %q", f)
	}

	key := f
	var value interface{} = nil
	if valueIndex == 0 {
		if keystack[len(keystack)-1] != "" {
			return fmt.Errorf("field %q requires a value separated by an '=' sign", key)
		}
	} else {
		key = f[0 : valueIndex-1]
		value = f[valueIndex:]
	}

	c.changed = true
	if value != nil {
		if key == contextKey {
			return c.setKey(value)
		}

		if isMagicField {
			var err error
			value, err = magicFieldValue(value.(string), c.in)
			if err != nil {
				return fmt.Errorf("error parsing %q value: %w", key, err)
			}
		}
	}

	destMap := *c.currentContextData
	isArray := false
	var subkey string
	for _, k := range keystack {
		if k == "" {
			isArray = true
			continue
		}
		if subkey != "" {
			var err error
			if isArray {
				destMap, err = addParamsSlice(destMap, subkey, k)
				isArray = false
			} else {
				destMap, err = addParamsMap(destMap, subkey)
			}
			if err != nil {
				return err
			}
		}
		subkey = k
	}

	if isArray {
		if value == nil {
			destMap[subkey] = []interface{}{}
		} else {
			if v, exists := destMap[subkey]; exists {
				if existSlice, ok := v.([]interface{}); ok {
					destMap[subkey] = append(existSlice, value)
				} else {
					return fmt.Errorf("expected array type under %q, got %T", subkey, v)
				}
			} else {
				destMap[subkey] = []interface{}{value}
			}
		}
	} else {
		if _, exists := destMap[subkey]; exists {
			return fmt.Errorf("unexpected override existing field under %q", subkey)
		}
		destMap[subkey] = value
	}
	return nil
}

func addParamsMap(m map[string]interface{}, key string) (map[string]interface{}, error) {
	if v, exists := m[key]; exists {
		if existMap, ok := v.(map[string]interface{}); ok {
			return existMap, nil
		} else {
			return nil, fmt.Errorf("expected map type under %q, got %T", key, v)
		}
	}
	newMap := make(map[string]interface{})
	m[key] = newMap
	return newMap, nil
}

func addParamsSlice(m map[string]interface{}, prevkey, newkey string) (map[string]interface{}, error) {
	if v, exists := m[prevkey]; exists {
		existSlice, ok := v.([]interface{})
		if !ok {
			return nil, fmt.Errorf("expected array type under %q, got %T", prevkey, v)
		}

		if len(existSlice) > 0 {
			lastItem := existSlice[len(existSlice)-1]
			if lastMap, ok := lastItem.(map[string]interface{}); ok {
				if _, keyExists := lastMap[newkey]; !keyExists {
					return lastMap, nil
				} else if reflect.TypeOf(lastMap[newkey]).Kind() == reflect.Slice {
					return lastMap, nil
				}
			}
		}
		newMap := make(map[string]interface{})
		m[prevkey] = append(existSlice, newMap)
		return newMap, nil
	}
	newMap := make(map[string]interface{})
	m[prevkey] = []interface{}{newMap}
	return newMap, nil
}

func jsonFieldValue(s string, stdin io.ReadCloser) (ldcontext.Context, error) {
	if strings.HasPrefix(s, "@") {
		b, err := readFile(s[1:], stdin)
		if err != nil {
			return ldcontext.Context{}, err
		}
		s = string(b)
	}

	var res ldcontext.Context
	err := json.Unmarshal([]byte(s), &res)
	if err != nil {
		return ldcontext.Context{}, fmt.Errorf("failed to unmarshal context: %w", err)
	}

	return res, nil
}

func magicFieldValue(v string, stdin io.ReadCloser) (interface{}, error) {
	if strings.HasPrefix(v, "@") {
		b, err := readFile(v[1:], stdin)
		if err != nil {
			return "", err
		}
		v = string(b)
	}

	if strings.HasPrefix(v, "[") || strings.HasPrefix(v, "{") {
		var data interface{}
		err := json.Unmarshal([]byte(v), &data)
		if err == nil {
			return data, nil
		}
	}

	switch strings.ToLower(v) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}

	if n, err := strconv.Atoi(v); err == nil {
		return n, nil
	}

	if n, err := strconv.ParseFloat(v, 64); err == nil {
		return n, nil
	}

	return v, nil
}

func readFile(f string, stdin io.ReadCloser) ([]byte, error) {
	var r io.ReadCloser
	switch f {
	case "-":
		r = stdin
	default:
		var err error
		r, err = os.Open(f)
		if err != nil {
			return nil, err
		}
	}
	defer r.Close()

	return io.ReadAll(r)
}
