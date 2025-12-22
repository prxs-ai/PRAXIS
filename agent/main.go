package main

import (
    "encoding/json"
    "log"
    "os"
    "strings"
)

// Architecture: The Agent is purely reactive. It maintains no network state.
// It listens on Stdin for work and replies on Stdout.
func main() {
    decoder := json.NewDecoder(os.Stdin)
    encoder := json.NewEncoder(os.Stdout)

    for {
	var req struct {
	    Method string   `json:"method"`
	    Params []string `json:"params"` // distinct from generic interface for simplicity
	    ID     int      `json:"id"`
	}

	// Block until a request arrives via the Daemon
	if err := decoder.Decode(&req); err != nil {
	    if err.Error() == "EOF" {
		return // Daemon closed the pipe
	    }
	    log.Printf("Agent Error: %v", err)
	    continue
	}

	// Business Logic (The actual "Service")
	var result string
	switch req.Method {
	case "uppercase":
	    if len(req.Params) > 0 {
		result = strings.ToUpper(req.Params[0])
	    }
	case "reverse":
	    if len(req.Params) > 0 {
		runes := []rune(req.Params[0])
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		    runes[i], runes[j] = runes[j], runes[i]
		}
		result = string(runes)
	    }
	default:
	    result = "unknown method"
	}

	// Respond to Daemon
	resp := struct {
	    Result string `json:"result"`
	    ID     int    `json:"id"`
	}{
	    Result: result,
	    ID:     req.ID,
	}

	if err := encoder.Encode(resp); err != nil {
	    log.Fatal(err)
	}
    }
}
