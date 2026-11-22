package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestNormalizeOpenAIToolSchema verifies tuple-form "items" (a JSON Schema
// draft-07 array of per-position schemas) is collapsed to a single schema
// before reaching an OpenAI-compatible provider. Strict providers (e.g. z.ai
// GLM) reject array-valued "items" with HTTP 400 "Invalid API parameter",
// which made every request fail once an MCP server such as webop exposed a
// tool with a coordinate tuple.
func TestNormalizeOpenAIToolSchema(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // "" means: returned unchanged (byte-identical)
	}{
		{
			name: "identical tuple with additionalItems false",
			in:   `{"type":"object","properties":{"c":{"type":"array","items":[{"type":"number"},{"type":"number"}],"additionalItems":false,"minItems":2,"maxItems":2}}}`,
			want: `{"type":"object","properties":{"c":{"type":"array","items":{"type":"number"},"minItems":2,"maxItems":2}}}`,
		},
		{
			name: "tuple without length bounds gets them from additionalItems false",
			in:   `{"type":"array","items":[{"type":"number"},{"type":"number"}],"additionalItems":false}`,
			want: `{"type":"array","items":{"type":"number"},"maxItems":2}`,
		},
		{
			name: "mixed tuple becomes anyOf",
			in:   `{"type":"array","items":[{"type":"string"},{"type":"number"}]}`,
			want: `{"type":"array","items":{"anyOf":[{"type":"string"},{"type":"number"}]}}`,
		},
		{
			name: "empty tuple dropped",
			in:   `{"type":"array","items":[]}`,
			want: `{"type":"array"}`,
		},
		{
			name: "nested inside anyOf and items",
			in:   `{"anyOf":[{"type":"array","items":{"type":"array","items":[{"type":"number"}]}}]}`,
			want: `{"anyOf":[{"type":"array","items":{"type":"array","items":{"type":"number"}}}]}`,
		},
		{
			name: "property literally named items is untouched",
			in:   `{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}}}`,
		},
		{
			name: "plain schema unchanged",
			in:   `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{}}`,
		},
		{
			name: "invalid JSON passed through",
			in:   `{not json`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeOpenAIToolSchema(json.RawMessage(tc.in))
			if tc.want == "" {
				if string(got) != tc.in {
					t.Errorf("got %s, want unchanged %s", got, tc.in)
				}
				return
			}
			var g, w any
			if err := json.Unmarshal(got, &g); err != nil {
				t.Fatalf("output not JSON: %s", got)
			}
			if err := json.Unmarshal([]byte(tc.want), &w); err != nil {
				t.Fatalf("bad want: %v", err)
			}
			if !reflect.DeepEqual(g, w) {
				t.Errorf("got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestOpenAIConvert_ToolSchemaNormalized verifies convertToOpenAIRequest
// applies the normalization to tool parameters.
func TestOpenAIConvert_ToolSchemaNormalized(t *testing.T) {
	c := &openaiClient{}
	req := c.convertToOpenAIRequest(&MessageRequest{
		Model: "m",
		Tools: []ToolSchema{{
			Name:        "browser_computer",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"coordinate":{"type":"array","items":[{"type":"number"},{"type":"number"}],"additionalItems":false}}}`),
		}},
	})
	var p map[string]any
	if err := json.Unmarshal(req.Tools[0].Function.Parameters, &p); err != nil {
		t.Fatal(err)
	}
	coord := p["properties"].(map[string]any)["coordinate"].(map[string]any)
	if _, isArr := coord["items"].([]any); isArr {
		t.Errorf("items still tuple-form: %v", coord)
	}
	if _, ok := coord["additionalItems"]; ok {
		t.Errorf("additionalItems not removed: %v", coord)
	}
}
