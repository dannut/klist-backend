package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	colorReset  = "\033[0m"
	colorBlue   = "\033[34m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorGray   = "\033[90m"
	colorBold   = "\033[1m"
)

func displayResults(cmds []Command, query string) {
	if len(cmds) == 0 {
		fmt.Printf("\n  No results for %q\n\n", query)
		return
	}

	width := termWidth()
	sep := strings.Repeat("─", width)

	// Show result count
	fmt.Printf("\n%s%s kli.st — %d results for %q%s\n", colorBold, colorBlue, len(cmds), query, colorReset)
	fmt.Println(colorGray + sep + colorReset)

	for _, cmd := range cmds {
		fmt.Printf("  %s%s%s\n", colorBlue, cmd.Syntax, colorReset)
		fmt.Printf("  %s%s%s\n", colorGray, cmd.Description, colorReset)
		fmt.Printf("  %s[%s]%s\n", colorYellow, cmd.Tool, colorReset)
		fmt.Println(colorGray + strings.Repeat("·", width) + colorReset)
	}

	// Summary line at the end for long results
	if len(cmds) >= 50 {
		fmt.Printf("%s%s  %d results total%s\n\n", colorBold, colorGreen, len(cmds), colorReset)
	} else {
		fmt.Println()
	}
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return 80
}
