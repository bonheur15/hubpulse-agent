package main

import (
	"fmt"
	"os"
)

const version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "hubpulse-agent: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "version", "--version", "-version":
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("command %q is not implemented yet", args[0])
	}
}

func printUsage() {
	fmt.Println("hubpulse-agent")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  hubpulse-agent version")
}
