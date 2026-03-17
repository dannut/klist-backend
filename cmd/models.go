package main

// Command is the JSON struct returned to frontend and CLI
type Command struct {
	Tool        string `json:"tool"`
	Syntax      string `json:"syntax"`
	Description string `json:"description"`
}
