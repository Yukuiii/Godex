package tools

import (
	"context"

	"github.com/sashabaranov/go-openai"
)

// ToolRouter is responsible for taking multi-agent API calls/messages
// and transmuting them into generic ToolCall definitions, then dispatching via the Registry.
type ToolRouter struct {
	registry *ToolRegistry
}

func NewToolRouter(registry *ToolRegistry) *ToolRouter {
	return &ToolRouter{registry: registry}
}

// BuildAndDispatch unifies the request layer before executing dispatch on the registry.
// GetAllToolSpecs forwards the collection request to the internal ToolRegistry.
func (r *ToolRouter) GetAllToolSpecs() []openai.Tool {
	return r.registry.GetAllToolSpecs()
}

func (r *ToolRouter) BuildAndDispatch(ctx context.Context, call *ToolCall) (ToolOutput, error) {
	// Construct the internal ToolInvocation referencing contextual variables.
	invocation := &ToolInvocation{
		CallID:        call.CallID,
		ToolName:      call.ToolName,
		ToolNamespace: call.ToolNamespace,
		Payload:       call.Payload,
		// Example: Link session pointer, turn IDs, tool gating channels
	}

	// Route to Registry for secure and sequenced execution.
	return r.registry.DispatchAny(ctx, invocation)
}
