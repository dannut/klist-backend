package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const defaultAPI = "https://kli.st"
const version    = "1.0.0"

func main() {
	apiURL := flag.String("api", defaultAPI, "Backend API URL")
	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		printHelp()
		os.Exit(0)
	}

	switch args[0] {
	case "search", "s":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: kli search <term>")
			os.Exit(1)
		}
		query := strings.Join(args[1:], " ")
		query = strings.TrimPrefix(query, "tool=")
		cmds, err := fetchCommands(*apiURL, query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		displayResults(cmds, query)

	case "version", "-v", "--version":
		fmt.Println("kli version " + version)

	case "help", "-h", "--help":
		printHelp()

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`
kli — kli.st CLI tool v` + version + `

Usage:
  kli search <term>          Search commands
  kli search tool=<name>     Search by tool name
  kli version                Show version
  kli help                   Show this help

Examples:
  kli search docker
  kli search "list containers"
  kli search tool=kubernetes
  kli search git commit

Options:
  --api <url>   Backend URL (default: http://localhost:8080)
`)
}
