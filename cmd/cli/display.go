package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func displayResults(cmds []Command, query string, page int) {
	if len(cmds) == 0 {
		fmt.Printf("\n  No results for %q\n\n", query)
		return
	}

	width := termWidth()
	rule := strings.Repeat("─", width)

	fmt.Printf("\n%s\n", rule)
	fmt.Printf("  kli.st  %d results for %q", len(cmds), query)
	if page > 1 {
		fmt.Printf("  (page %d)", page)
	}
	fmt.Printf("\n%s\n\n", rule)

	// syntaxCol = longest syntax in results, capped between 40 and 70
	syntaxCol := 40
	for _, cmd := range cmds {
		if l := len(cmd.Syntax); l > syntaxCol {
			syntaxCol = l
		}
	}
	if syntaxCol > 70 {
		syntaxCol = 70
	}

	descWidth := width - syntaxCol - 12
	if descWidth < 20 {
		descWidth = 20
	}

	for _, cmd := range cmds {
		syntax := cmd.Syntax
		if len(syntax) > syntaxCol {
			syntax = syntax[:syntaxCol-3] + "..."
		}

		desc := cmd.Description
		if len(desc) > descWidth {
			desc = desc[:descWidth-3] + "..."
		}

		fmt.Printf("  %-*s   —   %-*s  [%s]\n",
			syntaxCol, syntax,
			descWidth, desc,
			cmd.Tool,
		)
	}

	fmt.Println()

	if len(cmds) == 25 {
		fmt.Printf("  — more results available, use: kli search %q --page %d\n\n",
			query, page+1)
	}
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		if w > 200 {
			return 200
		}
		return w
	}
	return 80
}
