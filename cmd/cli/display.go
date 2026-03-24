package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	colorReset = "\033[0m"
	colorCyan  = "\033[36m"
	colorGray  = "\033[90m"
	colorBold  = "\033[1m"
	colorDim   = "\033[2m"
)

const syntaxCol = 36

func displayResults(cmds []Command, query string, page int) {
	if len(cmds) == 0 {
		fmt.Printf("\n  %sNo results for %q%s\n\n", colorGray, query, colorReset)
		return
	}

	width := termWidth()
	rule := colorDim + colorGray + strings.Repeat("─", width) + colorReset

	fmt.Printf("\n%s\n", rule)
	fmt.Printf("  %skli.st%s  %d results for %q",
		colorBold+colorCyan, colorReset, len(cmds), query)
	if page > 1 {
		fmt.Printf("  %s(page %d)%s", colorDim+colorGray, page, colorReset)
	}
	fmt.Printf("\n%s\n\n", rule)

	descWidth := width - syntaxCol - 8
	if descWidth < 20 {
		descWidth = 20
	}

	for _, cmd := range cmds {
		syntax := cmd.Syntax
		if len(syntax) > syntaxCol-1 {
			syntax = syntax[:syntaxCol-4] + "..."
		}

		desc := cmd.Description
		if len(desc) > descWidth {
			desc = desc[:descWidth-3] + "..."
		}

		fmt.Printf("  %s%-*s%s %s—%s %s%-*s%s  %s[%s]%s\n",
			colorBold+colorCyan, syntaxCol, syntax, colorReset,
			colorGray, colorReset,
			colorGray, descWidth, desc, colorReset,
			colorDim+colorGray, cmd.Tool, colorReset,
		)
	}

	fmt.Println()

	if len(cmds) == 50 {
		fmt.Printf("  %s— more results available, use: kli search %q --page %d%s\n\n",
			colorDim+colorGray, query, page+1, colorReset)
	}
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		if w > 120 {
			return 120
		}
		return w
	}
	return 80
}
