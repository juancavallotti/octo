//go:build !api

package main

// The default build adds no commands beyond the ones main.go dispatches. See
// commands_api.go for the one the api build adds, and why it is a tagged variable
// rather than a registration.
var extraCommands = map[string]func([]string) error{}
