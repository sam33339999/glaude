package api

import (
	"bytes"
	"encoding/json"
	"reflect"
)

// nonSchemaKeywords hold data values or name lists, not subschemas; their
// subtrees are never rewritten even if they contain an "items" key.
var nonSchemaKeywords = map[string]bool{
	"default":           true,
	"const":             true,
	"enum":              true,
	"examples":          true,
	"dependencies":      true, // draft-07: values may be string arrays
	"dependentRequired": true,
	"required":          true,
}

// nameMapKeywords map arbitrary user-chosen names to subschemas; keys inside
// them are names, not keywords, so a property called "enum" still recurses.
var nameMapKeywords = map[string]bool{
	"properties":        true,
	"patternProperties": true,
	"definitions":       true,
	"$defs":             true,
	"dependentSchemas":  true,
}

// normalizeOpenAIToolSchema rewrites JSON Schema constructs that strict
// OpenAI-compatible providers reject in function parameters. Currently it
// collapses tuple-form "items" (draft-07: an array of per-position schemas,
// usually paired with "additionalItems") into a single schema:
//
//   - all positions identical → that schema
//   - positions differ        → {"anyOf": [distinct schemas...]}
//   - empty array             → "items" removed
//
// "additionalItems" is dropped alongside: a schema value joins the anyOf, and
// false sets maxItems to the tuple length (unless maxItems is set) so the
// length bound survives. An absent additionalItems (default true) becomes
// slightly stricter — extra elements must match the collapsed schema — which
// is acceptable for a hint to the model.
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
	dec := json.NewDecoder(bytes.NewReader(schema))
	dec.UseNumber() // keep large integers exact
	if err := dec.Decode(&v); err != nil {
		return schema
	}
	if !collapseTupleItems(v, false) {
		return schema
	}
	out, err := json.Marshal(v)
	if err != nil {
		return schema
	}
	return out
}

// collapseTupleItems walks v in place and reports whether anything changed.
// nameMap is true when v is the value of a name→schema keyword such as
// "properties": its keys are names and every value is a schema.
func collapseTupleItems(v any, nameMap bool) bool {
	changed := false
	switch n := v.(type) {
	case map[string]any:
		if nameMap {
			for _, child := range n {
				if collapseTupleItems(child, false) {
					changed = true
				}
			}
			return changed
		}
		if tuple, ok := n["items"].([]any); ok {
			changed = true
			positions := tuple
			if extra, ok := n["additionalItems"].(map[string]any); ok {
				positions = append(append([]any{}, tuple...), extra)
			}
			switch distinct := distinctSchemas(positions); len(distinct) {
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
		for k, child := range n {
			if nonSchemaKeywords[k] {
				continue
			}
			if collapseTupleItems(child, nameMapKeywords[k]) {
				changed = true
			}
		}
	case []any:
		for _, child := range n {
			if collapseTupleItems(child, false) {
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
