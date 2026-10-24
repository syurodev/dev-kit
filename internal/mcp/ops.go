package mcp

import (
	"context"
	"devkit/internal/ops"
	"encoding/json"
	"fmt"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterOps projects agent-exposed declarations into SDK tools. Per-caller
// identity and result-budget middleware remain on the server, unchanged.
func RegisterOps(ms *sdk.Server, rt ops.Runtime, operations ...ops.Operation) {
	for _, op := range operations {
		d := op.Descriptor()
		if d.Exposure == ops.UI {
			continue
		}
		schema, err := jsonschema.ForType(op.InputType(), &jsonschema.ForOptions{})
		if err != nil {
			panic(fmt.Sprintf("operation %s input schema: %v", d.Name, err))
		}
		annotations := &sdk.ToolAnnotations{ReadOnlyHint: d.Risk == ops.Read}
		if d.Risk != ops.Read {
			annotations.DestructiveHint = boolPtr(d.Risk == ops.Destructive)
		}
		if d.Risk == ops.External {
			annotations.OpenWorldHint = boolPtr(true)
		}
		sdk.AddTool(ms, &sdk.Tool{Name: d.Name, Description: d.Agent.Description, Annotations: annotations, InputSchema: schema}, func(ctx context.Context, _ *sdk.CallToolRequest, in json.RawMessage) (*sdk.CallToolResult, any, error) {
			value, err := op.InvokeJSON(ctx, rt, in)
			if err != nil {
				return nil, nil, err
			}
			return nil, structured(value), nil
		})
	}
}
