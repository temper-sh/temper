package update

import "fmt"

const localAPI = "http://127.0.0.1:8080/v1"

func gatesFor(layoutID, role string) []Gate {
	switch role {
	case "coder":
		return []Gate{
			{
				Step:    "plain-completion",
				Command: fmt.Sprintf("curl -fsS -m 900 %s/chat/completions -H 'Content-Type: application/json' -d '{\"model\":\"%s\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly: ACCEPTANCE_OK\"}],\"max_tokens\":32,\"temperature\":0}' | jq -e '.choices[0].message.content == \"ACCEPTANCE_OK\"' >/dev/null", localAPI, layoutID),
			},
			{
				Step:    "streaming-tool-call",
				Command: fmt.Sprintf("curl -fsS -N -m 900 %s/chat/completions -H 'Content-Type: application/json' -d '{\"model\":\"%s\",\"stream\":true,\"max_tokens\":200,\"temperature\":0,\"tool_choice\":{\"type\":\"function\",\"function\":{\"name\":\"read\"}},\"messages\":[{\"role\":\"user\",\"content\":\"Read the file demo.txt and tell me what is on line 7.\"}],\"tools\":[{\"type\":\"function\",\"function\":{\"name\":\"read\",\"description\":\"Read a file\",\"parameters\":{\"type\":\"object\",\"properties\":{\"path\":{\"type\":\"string\"}},\"required\":[\"path\"]}}}]}' | sed -n 's/^data: //p' | sed '/^\\[DONE\\]$/d' | jq -e 'select(.choices[0].delta.tool_calls[0].function.name == \"read\")' >/dev/null", localAPI, layoutID),
			},
		}
	case "rerank":
		return []Gate{{
			Step:    "rerank-order-and-magnitude",
			Command: fmt.Sprintf("curl -fsS -m 900 %s/rerank -H 'Content-Type: application/json' -d '{\"model\":\"%s\",\"query\":\"How do I rotate the log files for a launchd service on macOS?\",\"documents\":[\"The recipe calls for two cups of flour, a pinch of salt, and butter at room temperature.\",\"Create /etc/newsyslog.d/llama-swap.conf with an owner:group field so newsyslog can rotate logs owned by your user account.\",\"Blue whales are the largest animals ever known to have lived on Earth.\"]}' | jq -e '((.results | sort_by(-.relevance_score) | .[0].index) == 1) and (([.results[].relevance_score] | max) > 0.001)' >/dev/null", localAPI, layoutID),
		}}
	default:
		return nil
	}
}
