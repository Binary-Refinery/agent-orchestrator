// This process fixture mirrors pinned OpenCode's models/debug-config command
// shapes. It performs no provider work and lets tests exercise real argv/cwd/env
// boundaries on every supported OS, including refusal and bounded-output paths.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		os.Exit(2)
	}
	if calls := os.Getenv("AO_PROBE_CALLS"); calls != "" {
		file, err := os.OpenFile(calls, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(3)
		}
		_, _ = fmt.Fprintln(file, strings.Join(os.Args[1:], " "))
		_ = file.Close()
	}
	switch strings.Join(os.Args[1:], " ") {
	case "models --verbose":
		data, err := os.ReadFile(os.Getenv("AO_PROBE_MODELS"))
		if err != nil {
			os.Exit(4)
		}
		_, _ = os.Stdout.Write(data)
	case "debug config":
		switch os.Getenv("AO_PROBE_CONFIG_MODE") {
		case "unsupported":
			fmt.Fprintln(os.Stderr, "unrecognized debug config secret-sentinel")
			os.Exit(1)
		case "error":
			fmt.Fprintln(os.Stdout, "secret-sentinel")
			fmt.Fprintln(os.Stderr, "secret-sentinel")
			os.Exit(1)
		case "large":
			fmt.Print(strings.Repeat("x", (2<<20)+1))
			return
		case "slow":
			time.Sleep(10 * time.Second)
		}
		data := []byte(os.Getenv("OPENCODE_CONFIG_CONTENT"))
		if len(data) == 0 {
			var err error
			data, err = os.ReadFile("native-config.json")
			if err != nil {
				os.Exit(5)
			}
		}
		_, _ = os.Stdout.Write(data)
	default:
		os.Exit(6)
	}
}
