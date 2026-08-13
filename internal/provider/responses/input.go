package responses

import (
	"encoding/json"
	"strings"

	"reasonix/internal/provider"
)

// inputOptions son las diferencias de vendor que cambian cómo se serializa el
// input. Se agrupan para que agregar una no reescriba cada llamada.
type inputOptions struct {
	vision             bool // adjuntar imágenes como input_image
	replaySearchItems  bool // reenviar los web_search_call ya ejecutados
	summary            bool // el reasoning item exige la lista summary
	encryptedReasoning bool // reenviar el reasoning item original del servidor
}

func (c *client) inputOptions() inputOptions {
	return inputOptions{
		vision: c.vision, replaySearchItems: c.webSearch,
		summary: c.caps.summaryRequired, encryptedReasoning: c.caps.encryptedReasoning,
	}
}

func messagesToInput(messages []provider.Message, opts inputOptions) []map[string]any {
	input := make([]map[string]any, 0, len(messages)*2)
	for _, message := range messages {
		switch message.Role {
		case provider.RoleSystem, provider.RoleUser:
			// Text-only turns keep the documented TextInput string shape.
			// Vision-capable user turns with attached images switch to the
			// InputItemList array form ({type:input_text} + {type:input_image})
			// so the text and every image ride the same message, matching the
			// MiMo/DashScope multimodal example. The system message is always
			// plain text: images only attach to user turns.
			if opts.vision && message.Role == provider.RoleUser && len(message.Images) > 0 {
				parts := make([]map[string]string, 0, len(message.Images)+1)
				if message.Content != "" {
					parts = append(parts, map[string]string{"type": "input_text", "text": message.Content})
				}
				for _, url := range message.Images {
					parts = append(parts, map[string]string{"type": "input_image", "image_url": url})
				}
				input = append(input, map[string]any{"role": "user", "content": parts})
			} else {
				input = append(input, map[string]any{"role": string(message.Role), "content": message.Content})
			}
		case provider.RoleAssistant:
			// Vendors that encrypt reasoning only accept the item they issued,
			// byte for byte. Rebuilding it from ReasoningContent would be
			// rejected, so the replayed items below carry the whole turn.
			if message.ReasoningContent != "" && !opts.encryptedReasoning {
				// Reasoning items: the OpenAI base format only needs
				// `content`. DashScope additionally requires a `summary`
				// list ("Invalid 'summary': summary is required and must be
				// a list for reasoning."). Other vendors (MiMo) do not
				// define summary in their schema; sending it leaks the
				// reasoning text into an extra field the server may echo
				// back into the model context, doubling chain-of-thought
				// each turn — so only send it where the wire demands it.
				item := map[string]any{
					"type":    "reasoning",
					"content": []map[string]string{{"type": "reasoning_text", "text": message.ReasoningContent}},
				}
				if message.ReasoningID != "" {
					// OpenAI Responses schema marks Reasoning.id required;
					// round-trip the provider-issued id when we captured one.
					item["id"] = message.ReasoningID
				}
				if message.ReasoningStatus != "" {
					item["status"] = message.ReasoningStatus
				}
				if opts.summary {
					item["summary"] = []map[string]string{{"type": "summary_text", "text": message.ReasoningContent}}
				}
				input = append(input, item)
			}
			if opts.replaySearchItems || opts.encryptedReasoning {
				for _, raw := range message.ResponsesItems {
					if item, ok := decodeReplayableItem(raw, opts); ok {
						input = append(input, item)
					}
				}
			}
			if message.Content != "" || len(message.ToolCalls) == 0 {
				input = append(input, map[string]any{"role": "assistant", "content": message.Content})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type": "function_call", "call_id": call.ID,
					"name": call.Name, "arguments": call.Arguments,
				})
			}
		case provider.RoleTool:
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content,
			})
		}
	}
	return input
}

// decodeReplayableItem valida un output item guardado antes de reenviarlo.
// Cada familia tiene su propia condición: un web_search_call solo se replica si
// terminó, y un reasoning solo si trae el estado cifrado que el servidor exige
// cuando no guarda la respuesta.
func decodeReplayableItem(raw json.RawMessage, opts inputOptions) (map[string]any, bool) {
	if len(raw) == 0 || len(raw) > maxReplayableSearchItemBytes || !json.Valid(raw) {
		return nil, false
	}
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, false
	}
	switch item["type"] {
	case "web_search_call":
		if !opts.replaySearchItems {
			return nil, false
		}
		id, _ := item["id"].(string)
		status, _ := item["status"].(string)
		if strings.TrimSpace(id) == "" || status != "completed" {
			return nil, false
		}
		return item, true
	case "reasoning":
		if !opts.encryptedReasoning {
			return nil, false
		}
		encrypted, _ := item["encrypted_content"].(string)
		if strings.TrimSpace(encrypted) == "" {
			return nil, false
		}
		return item, true
	default:
		return nil, false
	}
}
