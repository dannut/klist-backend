package main

import (
	"os"
	"strconv"
)

// getenv returns the value of the environment variable key,
// or fallback if the variable is not set or empty.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseIntParam parses a string query param to int, returning fallback on error.
func parseIntParam(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}
