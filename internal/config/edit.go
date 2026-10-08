package config

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Get returns the value of a dotted key such as filter.only_unread in cfg.
// Scalars are returned as is, lists and mappings as YAML.
func Get(cfg *Config, key string) (string, error) {
	var root yaml.Node
	if err := root.Encode(cfg); err != nil {
		return "", err
	}
	n := &root
	for p := range strings.SplitSeq(key, ".") {
		i := mappingIndex(n, p)
		if i < 0 {
			return "", fmt.Errorf("unknown key %q", key)
		}
		n = n.Content[i+1]
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value, nil
	}
	b, err := yaml.Marshal(n)
	return strings.TrimSuffix(string(b), "\n"), err
}

// Set sets a dotted key in the config file at path to value, which is parsed
// as YAML (e.g. true, 60, [a, b]). The result must be a valid config.
func Set(path, key, value string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := setBytes(path, src, key, value)
	if err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, fi.Mode().Perm())
}

func setBytes(path string, src []byte, key, value string) ([]byte, error) {
	defaults, err := ParseBytes(path, nil)
	if err != nil {
		return nil, err
	}
	if _, err := Get(defaults, key); err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	val, err := parseValue(value)
	if err != nil {
		return nil, err
	}

	var keyNode, old *yaml.Node
	m := doc.Content[0]
	parts := strings.Split(key, ".")
	for i, p := range parts {
		if m.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s is not a mapping", strings.Join(parts[:i], "."))
		}
		last := i == len(parts)-1
		j := mappingIndex(m, p)
		if j < 0 {
			child := val
			if !last {
				child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p}, child)
			m = child
			continue
		}
		if last {
			keyNode, old = m.Content[j], m.Content[j+1]
			val.HeadComment, val.LineComment, val.FootComment = old.HeadComment, old.LineComment, old.FootComment
			m.Content[j+1] = val
		}
		m = m.Content[j+1]
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	enc.Close()
	want, err := ParseBytes(path, buf.Bytes())
	if err != nil {
		return nil, err
	}

	// Re-encoding drops blank lines, so prefer rewriting only the value's
	// text when that yields the same config.
	if old != nil {
		if edited, ok := replaceInline(src, keyNode, old, val); ok {
			if got, err := ParseBytes(path, edited); err == nil && reflect.DeepEqual(got, want) {
				return edited, nil
			}
		}
	}
	return buf.Bytes(), nil
}

func parseValue(s string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil {
		return nil, fmt.Errorf("value %q: %w", s, err)
	}
	if doc.Kind == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "", Style: yaml.DoubleQuotedStyle}, nil
	}
	return doc.Content[0], nil
}

// replaceInline replaces old, a value written on the same line as its key, with
// val written in flow style, keeping the rest of the line.
func replaceInline(src []byte, keyNode, old, val *yaml.Node) ([]byte, bool) {
	if old.Line != keyNode.Line {
		return nil, false
	}
	lines := strings.SplitAfter(string(src), "\n")
	line := lines[old.Line-1]
	start := old.Column - 1
	if start > len(line) {
		return nil, false
	}
	end := len(strings.TrimRight(line, "\r\n"))
	if c := cmp.Or(old.LineComment, keyNode.LineComment); c != "" {
		i := strings.LastIndex(line[:end], c)
		if i < start {
			return nil, false
		}
		end = len(strings.TrimRight(line[:i], " \t"))
	}

	flow := *val
	flow.HeadComment, flow.LineComment, flow.FootComment = "", "", ""
	if flow.Kind != yaml.ScalarNode {
		flow.Style |= yaml.FlowStyle
	}
	b, err := yaml.Marshal(&flow)
	if err != nil {
		return nil, false
	}
	text := strings.TrimSuffix(string(b), "\n")
	if strings.Contains(text, "\n") {
		return nil, false
	}
	lines[old.Line-1] = line[:start] + text + line[end:]
	return []byte(strings.Join(lines, "")), true
}

func mappingIndex(m *yaml.Node, key string) int {
	if m.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}
