// Package backbone verifies operator-pinned, data-only Engineering Backbone
// context. It never imports or executes code from the candidate repository.
package backbone

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Resource limits apply before parsing, file reads and prompt construction.
const (
	MaxJSONBytes     = 1 << 20
	MaxDocumentBytes = 1 << 20
	MaxPromptBytes   = 4 << 20
	MaxDocuments     = 256
)

// DecodeStrict rejects duplicate, unknown and case-folded fields, missing
// required fields, null scalar substitutes and excessive JSON depth. Struct
// fields tagged omitempty may be absent, but null requires bbnullable:"true".
// RawMessage fields are bounded JSON with duplicates checked recursively; their
// own schema must subsequently be checked before use.
func DecodeStrict(data []byte, target any) error {
	value, err := parseJSON(data)
	if err != nil {
		return err
	}
	typ := reflect.TypeOf(target)
	if typ == nil || typ.Kind() != reflect.Pointer || reflect.ValueOf(target).IsNil() {
		return fmt.Errorf("backbone JSON target must be a nonnil pointer")
	}
	if err := shape(value, typ.Elem(), false); err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("invalid Backbone JSON value: %w", err)
	}
	return nil
}

func parseJSON(data []byte) (any, error) {
	if len(data) == 0 || len(data) > MaxJSONBytes || !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid Backbone JSON size or UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := jsonValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("trailing Backbone JSON data")
	}
	return value, nil
}

func jsonValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, fmt.Errorf("backbone JSON nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid Backbone JSON")
	}
	switch token := token.(type) {
	case json.Delim:
		switch token {
		case '{':
			value := map[string]any{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return nil, fmt.Errorf("invalid Backbone JSON key")
				}
				name, ok := key.(string)
				if !ok || strings.ContainsRune(name, utf8.RuneError) {
					return nil, fmt.Errorf("invalid Backbone JSON key")
				}
				if _, exists := value[name]; exists {
					return nil, fmt.Errorf("duplicate Backbone JSON field %q", name)
				}
				if len(value) >= 1024 {
					return nil, fmt.Errorf("backbone JSON object exceeds limit")
				}
				child, err := jsonValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				value[name] = child
			}
			if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
				return nil, fmt.Errorf("invalid Backbone JSON object")
			}
			return value, nil
		case '[':
			value := []any{}
			for decoder.More() {
				if len(value) >= 4096 {
					return nil, fmt.Errorf("backbone JSON array exceeds limit")
				}
				child, err := jsonValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				value = append(value, child)
			}
			if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
				return nil, fmt.Errorf("invalid Backbone JSON array")
			}
			return value, nil
		default:
			return nil, fmt.Errorf("invalid Backbone JSON delimiter")
		}
	case string:
		if strings.ContainsRune(token, utf8.RuneError) {
			return nil, fmt.Errorf("invalid Backbone JSON Unicode scalar")
		}
		return token, nil
	case json.Number:
		if _, err := strconv.ParseInt(string(token), 10, 64); err != nil {
			return nil, fmt.Errorf("backbone JSON requires bounded integers")
		}
		return token, nil
	case nil, bool:
		return token, nil
	default:
		return nil, fmt.Errorf("invalid Backbone JSON token")
	}
}

var rawMessageType = reflect.TypeFor[json.RawMessage]()

func shape(value any, typ reflect.Type, nullable bool) error {
	if value == nil {
		if nullable {
			return nil
		}
		return fmt.Errorf("null is not a Backbone value")
	}
	if typ == rawMessageType {
		return nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("backbone JSON requires an object")
		}
		fields := map[string]reflect.StructField{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = field
		}
		for name, child := range object {
			field, ok := fields[name]
			if !ok {
				return fmt.Errorf("unknown Backbone JSON field %q", name)
			}
			if err := shape(child, field.Type, field.Tag.Get("bbnullable") == "true"); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		for name, field := range fields {
			if _, ok := object[name]; !ok && !strings.Contains(field.Tag.Get("json"), ",omitempty") {
				return fmt.Errorf("missing Backbone JSON field %q", name)
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok || typ.Key().Kind() != reflect.String {
			return fmt.Errorf("backbone JSON requires a string-keyed object")
		}
		for _, child := range object {
			if err := shape(child, typ.Elem(), false); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("backbone JSON requires an array")
		}
		for _, child := range array {
			if err := shape(child, typ.Elem(), false); err != nil {
				return err
			}
		}
	case reflect.String:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("backbone JSON requires a string")
		}
	case reflect.Bool:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("backbone JSON requires a boolean")
		}
	case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64:
		if _, ok := value.(json.Number); !ok {
			return fmt.Errorf("backbone JSON requires an integer")
		}
	default:
		return fmt.Errorf("unsupported Backbone JSON schema type")
	}
	return nil
}

// canonicalJSON matches the producer's sorted, UTF-8, compact JSON encoding.
func canonicalJSON(value any) []byte {
	var out bytes.Buffer
	var write func(any)
	write = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(value))
			for key := range value {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			out.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					out.WriteByte(',')
				}
				write(key)
				out.WriteByte(':')
				write(value[key])
			}
			out.WriteByte('}')
		case []any:
			out.WriteByte('[')
			for i, child := range value {
				if i > 0 {
					out.WriteByte(',')
				}
				write(child)
			}
			out.WriteByte(']')
		case string:
			out.WriteByte('"')
			for _, r := range value {
				switch r {
				case '"', '\\':
					out.WriteByte('\\')
					out.WriteRune(r)
				case '\b':
					out.WriteString(`\b`)
				case '\f':
					out.WriteString(`\f`)
				case '\n':
					out.WriteString(`\n`)
				case '\r':
					out.WriteString(`\r`)
				case '\t':
					out.WriteString(`\t`)
				default:
					if r < 32 {
						fmt.Fprintf(&out, `\u%04x`, r)
					} else {
						out.WriteRune(r)
					}
				}
			}
			out.WriteByte('"')
		case json.Number:
			out.WriteString(string(value))
		case bool:
			out.WriteString(strconv.FormatBool(value))
		case nil:
			out.WriteString("null")
		}
	}
	write(value)
	return out.Bytes()
}
