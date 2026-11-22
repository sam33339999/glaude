package api

import (
	"bytes"
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
			name: "data and name-list keywords are not rewritten",
			in:   `{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}},"dependencies":{"items":["count"]},"default":{"items":["a","b"]},"enum":[{"items":[1]}],"examples":[{"items":[]}]}`,
		},
		{
			name: "rewrite elsewhere leaves data keywords intact",
			in:   `{"type":"object","default":{"items":["a","b"]},"properties":{"c":{"type":"array","items":[{"type":"number"}]}}}`,
			want: `{"type":"object","default":{"items":["a","b"]},"properties":{"c":{"type":"array","items":{"type":"number"}}}}`,
		},
		{
			name: "additionalItems schema joins anyOf",
			in:   `{"type":"array","items":[{"type":"string"}],"additionalItems":{"type":"integer"}}`,
			want: `{"type":"array","items":{"anyOf":[{"type":"string"},{"type":"integer"}]}}`,
		},
		{
			name: "large integers keep precision",
			in:   `{"type":"array","maximum":9007199254740993,"items":[{"type":"number"}]}`,
			want: `{"type":"array","maximum":9007199254740993,"items":{"type":"number"}}`,
		},
		{
			name: "property named like a data keyword still recurses",
			in:   `{"type":"object","properties":{"enum":{"type":"array","items":[{"type":"number"}]},"default":{"type":"array","items":[{"type":"string"}]}}}`,
			want: `{"type":"object","properties":{"enum":{"type":"array","items":{"type":"number"}},"default":{"type":"array","items":{"type":"string"}}}}`,
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
			g, err := decodeNumbers(got)
			if err != nil {
				t.Fatalf("output not JSON: %s", got)
			}
			w, err := decodeNumbers([]byte(tc.want))
			if err != nil {
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

// decodeNumbers decodes JSON keeping numbers as json.Number so precision
// differences are visible to reflect.DeepEqual.
func decodeNumbers(b []byte) (any, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	err := dec.Decode(&v)
	return v, err
}
