package main

import "os"

// getenv returns the value of the environment variable key,
// or fallback if the variable is not set or empty.
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
