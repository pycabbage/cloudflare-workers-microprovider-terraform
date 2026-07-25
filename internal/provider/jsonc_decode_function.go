package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/tailscale/hujson"
)

type jsoncDecodeFunction struct{}

var _ function.Function = (*jsoncDecodeFunction)(nil)

func NewJSONCDecodeFunction() function.Function {
	return &jsoncDecodeFunction{}
}

func (f *jsoncDecodeFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "jsonc_decode"
}

func (f *jsoncDecodeFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary: "Decodes JSON-with-comments (JSONC) text into a value, like jsondecode() but tolerant of comments and trailing commas.",
		Description: "Behaves like Terraform's built-in jsondecode(), but first strips the " +
			"JSONC-only syntax extensions used by files such as Cloudflare's wrangler.jsonc: " +
			"// line comments, /* */ block comments, and trailing commas in objects and " +
			"arrays. The content is typically read from disk with the built-in file() " +
			"function; this function does no file I/O of its own, only parsing. The result " +
			"is a genuine decoded value - an object, tuple, string, number, bool, or null - " +
			"navigable directly with `.attr` / `[index]` syntax, not a JSON string requiring " +
			"a second jsondecode() call.",
		Parameters: []function.Parameter{
			function.StringParameter{
				Name: "content",
				Description: "JSONC text to decode, typically the result of " +
					"file(\"${path.module}/wrangler.jsonc\") or similar.",
			},
		},
		Return: function.DynamicReturn{},
	}
}

func (f *jsoncDecodeFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var content string
	resp.Error = function.ConcatFuncErrors(resp.Error, req.Arguments.Get(ctx, &content))
	if resp.Error != nil {
		return
	}

	value, err := decodeJSONC(content)
	if err != nil {
		resp.Error = function.ConcatFuncErrors(resp.Error, function.NewArgumentFuncError(0, err.Error()))
		return
	}

	resp.Error = function.ConcatFuncErrors(resp.Error, resp.Result.Set(ctx, value))
}

// decodeJSONC strips JSONC-only syntax (// and /* */ comments, trailing
// commas) via github.com/tailscale/hujson: content is parsed into a HuJSON
// AST with hujson.Parse, standardized in place with (*Value).Standardize -
// which replaces comments and trailing commas with a space character rather
// than re-marshaling, preserving line numbers/byte offsets and, in turn, the
// original number text for the precision-sensitive decode step below - and
// packed back into plain JSON bytes with (Value).Pack. Unlike the former
// tidwall/jsonc-based approach, hujson.Parse is a real parser, so malformed
// input is caught right there with a line/column-annotated error, rather
// than surfacing later as an opaque encoding/json error over already-mangled
// bytes.
//
// Numbers are decoded via json.Decoder.UseNumber() rather than a plain
// json.Unmarshal into `any`, which would silently convert every number to a
// lossy float64. Each json.Number's decimal text is instead parsed directly
// into a *big.Float with generous precision, preserving exact value even for
// integers beyond 2^53.
//
// JSON objects become framework Object values and JSON arrays become
// framework Tuple values (not List: JSON arrays may mix element types
// positionally, same as HCL's own jsondecode()/cty representation). Every
// nested value is wrapped as Dynamic so the surrounding Object/Tuple's
// attribute/element types are uniformly DynamicType, since a JSON value's
// shape isn't statically knowable ahead of time.
func decodeJSONC(content string) (attr.Value, error) {
	ast, err := hujson.Parse([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("content is not valid JSONC: %w", err)
	}
	ast.Standardize()
	stripped := ast.Pack()

	dec := json.NewDecoder(bytes.NewReader(stripped))
	dec.UseNumber()

	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("content is not valid JSON after stripping comments/trailing commas: %w", err)
	}

	// A valid JSON document must contain exactly one top-level value;
	// reject trailing garbage after it (dec.Decode only consumes the first
	// value and would otherwise silently ignore the rest).
	if dec.More() {
		return nil, fmt.Errorf("content contains additional data after the first JSON value")
	}

	return toDynamicValue(decoded)
}

// toDynamicValue converts a value produced by decodeJSONC's
// json.Decoder(UseNumber) into a types.Dynamic wrapping the appropriate
// concrete framework attr.Value.
func toDynamicValue(v any) (attr.Value, error) {
	switch val := v.(type) {
	case nil:
		return types.DynamicNull(), nil
	case bool:
		return types.DynamicValue(types.BoolValue(val)), nil
	case string:
		return types.DynamicValue(types.StringValue(val)), nil
	case json.Number:
		f := new(big.Float).SetPrec(512)
		if _, _, err := f.Parse(val.String(), 10); err != nil {
			return nil, fmt.Errorf("failed to parse number %q: %w", val.String(), err)
		}
		return types.DynamicValue(types.NumberValue(f)), nil
	case map[string]any:
		attrTypes := make(map[string]attr.Type, len(val))
		attrValues := make(map[string]attr.Value, len(val))
		for key, elem := range val {
			elemValue, err := toDynamicValue(elem)
			if err != nil {
				return nil, err
			}
			attrTypes[key] = types.DynamicType
			attrValues[key] = elemValue
		}
		obj, diags := types.ObjectValue(attrTypes, attrValues)
		if diags.HasError() {
			return nil, fmt.Errorf("failed to build object value: %s", diags)
		}
		return types.DynamicValue(obj), nil
	case []any:
		elemTypes := make([]attr.Type, len(val))
		elemValues := make([]attr.Value, len(val))
		for i, elem := range val {
			elemValue, err := toDynamicValue(elem)
			if err != nil {
				return nil, err
			}
			elemTypes[i] = types.DynamicType
			elemValues[i] = elemValue
		}
		tuple, diags := types.TupleValue(elemTypes, elemValues)
		if diags.HasError() {
			return nil, fmt.Errorf("failed to build tuple value: %s", diags)
		}
		return types.DynamicValue(tuple), nil
	default:
		return nil, fmt.Errorf("unsupported decoded JSON type %T", v)
	}
}
