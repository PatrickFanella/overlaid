package bot

import (
	"encoding/json"
	"strings"
)

// Inspect every key in JSON text, including duplicate object members that
// would disappear when decoding into a map. Plain non-JSON text is not scanned.
func containsJSONSecretKey(response string) bool {
	if !json.Valid([]byte(response)) {
		return false
	}
	decoder := json.NewDecoder(strings.NewReader(response))
	decoder.UseNumber()
	// k: object key, v: object value, a: array element.
	stack := []byte{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{', '[':
				if len(stack) > 0 && stack[len(stack)-1] == 'v' {
					stack[len(stack)-1] = 'k'
				}
				if delim == '{' {
					stack = append(stack, 'k')
				} else {
					stack = append(stack, 'a')
				}
			case '}', ']':
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if len(stack) == 0 {
			continue
		}
		switch stack[len(stack)-1] {
		case 'k':
			if key, ok := token.(string); ok && isForbiddenKey(key) {
				return true
			}
			stack[len(stack)-1] = 'v'
		case 'v':
			stack[len(stack)-1] = 'k'
		}
	}
}

func isForbiddenKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	for _, needle := range []string{"secret", "token", "password", "authorization", "cookie"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	compact := strings.ReplaceAll(strings.ReplaceAll(lower, "_", ""), "-", "")
	if strings.Contains(compact, "apikey") || strings.Contains(compact, "privatekey") {
		return true
	}
	return false
}
