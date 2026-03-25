package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const defaultAPI = "https://kli.st"

func main() {
	flag.Usage = printHelp
	apiURL := flag.String("api", defaultAPI, "Backend API URL")
	page := flag.Int("page", 1, "Page number for results")
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
		cmds, err := fetchCommands(*apiURL, query, *page)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		displayResults(cmds, query, *page)

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
kli — instant search for DevOps CLI commands
5000+ commands across 20 tools, accessible from your terminal.

USAGE
  kli <command> [flags]

COMMANDS
  search, s   Search commands by tool name or keyword

FLAGS
  --page <n>    Page number for paginated results  (default: 1)
  --api  <url>  Override the backend API URL       (default: https://kli.st)

EXAMPLES
  kli search docker                      all docker commands
  kli search "list running containers"   natural language search
  kli search tool=kubernetes             filter by tool
  kli search git commit                  multi-word search
  kli search nginx --page 2              paginate results

SUPPORTED TOOLS
  docker   kubectl  git       terraform  aws     gcloud
  helm     ansible  linux     nginx      bash    vim
  psql     redis    mongo     aws        pip     npm  ...

  https://kli.st
`)
}
