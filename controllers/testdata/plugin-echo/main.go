package main

import (
	"encoding/json"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		panic("Need at least one argument")
	}

	arg := os.Args[1]

	var d struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exitCode"`
	}

	if err := json.Unmarshal([]byte(arg), &d); err != nil {
		panic("Failed to unmarshal JSON: " + err.Error())
	}

	if d.Stdout != "" {
		os.Stdout.WriteString(d.Stdout)
	}
	if d.Stderr != "" {
		os.Stderr.WriteString(d.Stderr)
	}
	os.Exit(d.ExitCode)
}
