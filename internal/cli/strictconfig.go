package cli

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var yamlUnmarshaler = reflect.TypeFor[yaml.Unmarshaler]()

func checkKeys(node *yaml.Node, t reflect.Type, path string) error {
	if node.Kind == yaml.DocumentNode {
		for _, c := range node.Content {
			if err := checkKeys(c, t, path); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	if reflect.PointerTo(t).Implements(yamlUnmarshaler) {
		return nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return checkKeys(node, t.Elem(), path)
	case reflect.Slice, reflect.Array:
		if node.Kind != yaml.SequenceNode {
			return nil
		}
		for i, c := range node.Content {
			if err := checkKeys(c, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		if node.Kind != yaml.MappingNode {
			return nil
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if err := checkKeys(node.Content[i+1], t.Elem(), joinKey(path, node.Content[i].Value)); err != nil {
				return err
			}
		}
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return nil
		}
		fields := yamlFields(t)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value == "<<" {
				continue
			}
			ft, ok := fields[key.Value]
			if !ok {
				return unknownKey(key, path, fields)
			}
			if err := checkKeys(node.Content[i+1], ft, joinKey(path, key.Value)); err != nil {
				return err
			}
		}
	default:
	}
	return nil
}

func yamlFields(t reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out[name] = f.Type
	}
	return out
}

func unknownKey(key *yaml.Node, path string, fields map[string]reflect.Type) error {
	known := make([]string, 0, len(fields))
	for k := range fields {
		known = append(known, k)
	}
	sort.Strings(known)
	msg := fmt.Sprintf("line %d: %s: unknown key", key.Line, joinKey(path, key.Value))
	if s := suggestKey(key.Value, known); s != "" {
		return fmt.Errorf("%s, did you mean %q?", msg, s)
	}
	msg += " (known: " + strings.Join(known, ", ") + ")"
	if _, ok := fields["meta"]; ok {
		msg += "; your own metadata goes under meta:"
	}
	return fmt.Errorf("%s", msg)
}

func suggestKey(key string, known []string) string {
	best, bestDist := "", -1
	for _, k := range known {
		d := editDistance(strings.ToLower(key), k)
		if d <= 2 && 3*d <= len(key) && (bestDist < 0 || d < bestDist) {
			best, bestDist = k, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func joinKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
