package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const defaultAPI = "https://kli.st"

// extractPage removes --page <n> or --page=<n> from args and returns cleaned args + page value.
func extractPage(osArgs []string) ([]string, int) {
	page := 1
	out := make([]string, 0, len(osArgs))
	for i := 0; i < len(osArgs); i++ {
		arg := osArgs[i]
		if arg == "--page" || arg == "-page" {
			if i+1 < len(osArgs) {
				if n, err := strconv.Atoi(osArgs[i+1]); err == nil {
					page = n
					i++
					continue
				}
			}
		}
		if strings.HasPrefix(arg, "--page=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(arg, "--page=")); err == nil {
				page = n
				continue
			}
		}
		out = append(out, arg)
	}
	return out, page
}

func main() {
	cleanArgs, page := extractPage(os.Args[1:])
	os.Args = append([]string{os.Args[0]}, cleanArgs...)

	flag.Usage = printHelp
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
		cmds, err := fetchCommands(*apiURL, query, page)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		displayResults(cmds, query, page)

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
  helm     ansible  linux     nginx      argocd  prometheus
  grafana  vault    redis     kafka      harbor  flux  ...

  https://kli.st
`)
}
