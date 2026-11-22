package api

import (
	"encoding/json"
	"reflect"
)

// normalizeOpenAIToolSchema rewrites JSON Schema constructs that strict
// OpenAI-compatible providers reject in function parameters. Currently it
// collapses tuple-form "items" (draft-07: an array of per-position schemas,
// usually paired with "additionalItems") into a single schema:
//
//   - all positions identical → that schema
//   - positions differ        → {"anyOf": [distinct schemas...]}
//   - empty array             → "items" removed
//
// "additionalItems" is dropped alongside; when it was false and no maxItems
// is set, maxItems is set to the tuple length so the length bound survives.
//
// z.ai (GLM) returns HTTP 400 code 1210 "Invalid API parameter" for
// array-valued "items", failing every request once an MCP tool uses it.
// The schema is returned unchanged (byte-identical) when nothing needs
// rewriting or it is not valid JSON.
func normalizeOpenAIToolSchema(schema json.RawMessage) json.RawMessage {
	if len(schema) == 0 {
		return schema
	}
	var v any
	if err := json.Unmarshal(schema, &v); err != nil {
		return schema
	}
	if !collapseTupleItems(v) {
		return schema
	}
	out, err := json.Marshal(v)
	if err != nil {
		return schema
	}
	return out
}

// collapseTupleItems walks v in place and reports whether anything changed.
func collapseTupleItems(v any) bool {
	changed := false
	switch n := v.(type) {
	case map[string]any:
		if tuple, ok := n["items"].([]any); ok {
			changed = true
			switch distinct := distinctSchemas(tuple); len(distinct) {
			case 0:
				delete(n, "items")
			case 1:
				n["items"] = distinct[0]
			default:
				n["items"] = map[string]any{"anyOf": distinct}
			}
			if ai, ok := n["additionalItems"].(bool); ok && !ai {
				if _, has := n["maxItems"]; !has {
					n["maxItems"] = len(tuple)
				}
			}
			delete(n, "additionalItems")
		}
		for _, child := range n {
			if collapseTupleItems(child) {
				changed = true
			}
		}
	case []any:
		for _, child := range n {
			if collapseTupleItems(child) {
				changed = true
			}
		}
	}
	return changed
}

// distinctSchemas returns the elements of s with deep-equal duplicates
// removed, preserving first-occurrence order.
func distinctSchemas(s []any) []any {
	var out []any
	for _, e := range s {
		dup := false
		for _, o := range out {
			if reflect.DeepEqual(e, o) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, e)
		}
	}
	return out
}
