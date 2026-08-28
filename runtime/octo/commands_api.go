//go:build api

package main

// The api build answers one more command than the others.
//
// It is a build-tagged variable rather than a registration from an init, so it
// follows the same shape as providers_api.go beside it: what a binary can do is
// decided at compile time, in one place, by the tag. A binary with no api
// provider has nothing to verify, and a command that said so would be worse than
// one that is simply absent.
var extraCommands = map[string]func([]string) error{
	"verify-platform-api": verifyPlatformAPICommand,
}
