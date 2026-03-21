package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"hubpulse-agent/internal/app"
	"hubpulse-agent/internal/config"
	"hubpulse-agent/internal/version"
)

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
		fmt.Println(version.AgentVersion)
		return nil
	case "run":
		return runCommand(args[1:])
	case "status":
		return statusCommand(args[1:])
	case "validate-config":
		return validateConfigCommand(args[1:])
	case "print-default-config":
		return app.PrintDefaultConfig()
	case "init-config":
		return initConfigCommand(args[1:])
	case "update-config":
		return updateConfigCommand(args[1:])
	case "self-update":
		return app.SelfUpdate()
	default:
		return fmt.Errorf("unsupported command %q", args[0])
	}
}

func printUsage() {
	fmt.Println("hubpulse-agent")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  hubpulse-agent run [--config PATH] [--once] [--print-snapshot]")
	fmt.Println("  hubpulse-agent status [--config PATH]")
	fmt.Println("  hubpulse-agent validate-config [--config PATH]")
	fmt.Println("  hubpulse-agent print-default-config")
	fmt.Println("  hubpulse-agent init-config [--config PATH] [--force]")
	fmt.Println("  hubpulse-agent update-config [--config PATH] <base64-config>")
	fmt.Println("  hubpulse-agent self-update")
	fmt.Println("  hubpulse-agent version")
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the agent config file")
	once := fs.Bool("once", false, "collect and send one snapshot, then exit")
	printSnapshot := fs.Bool("print-snapshot", false, "print the collected snapshot instead of sending it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return app.Run(context.Background(), *configPath, *once, *printSnapshot)
}

func statusCommand(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the agent config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return app.Run(context.Background(), *configPath, true, true)
}

func validateConfigCommand(args []string) error {
	fs := flag.NewFlagSet("validate-config", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the agent config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return app.ValidateConfig(config.ResolveConfigPath(*configPath))
}

func initConfigCommand(args []string) error {
	fs := flag.NewFlagSet("init-config", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the agent config file")
	force := fs.Bool("force", false, "overwrite an existing config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return app.InitConfig(config.ResolveConfigPath(*configPath), *force)
}

func updateConfigCommand(args []string) error {
	fs := flag.NewFlagSet("update-config", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the agent config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("update-config requires exactly one base64 payload argument")
	}
	return app.UpdateConfig(config.ResolveConfigPath(*configPath), fs.Arg(0))
}
