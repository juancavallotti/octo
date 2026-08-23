package redisx

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// A URL that does not parse must not reach the log with its password in it. This
// is the whole reason redactURL exists: go-redis echoes the string it was given,
// and a pod log outlives the process.
func TestOpenDoesNotEchoTheURL(t *testing.T) {
	const url = "redis://user:hunter2@cache.example:notaport"

	_, err := Open(context.Background(), url)
	if err == nil {
		t.Fatal("want an error for an unparseable url")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("error leaks the password: %v", err)
	}
	if strings.Contains(err.Error(), url) {
		t.Errorf("error echoes the url: %v", err)
	}
	// The reason still has to survive the redaction, or the message is useless.
	if !strings.Contains(err.Error(), "not a valid redis") {
		t.Errorf("error does not say what was wrong: %v", err)
	}
}

// A URL with nothing sensitive in it is left alone: there is no reason to hide a
// host and port from whoever has to fix them.
func TestRedactURLLeavesUnrelatedErrorsAlone(t *testing.T) {
	err := errors.New("invalid port")
	if got := redactURL(err, "redis://cache:6379"); got != err {
		t.Errorf("redactURL rewrote an error that did not contain the url: %v", got)
	}
}

// An address nothing is listening on has to fail rather than hang: the client is
// lazy, so without the PING in Open this would return a usable-looking handle.
func TestOpenFailsOnAnUnreachableServer(t *testing.T) {
	// Port 1 on the loopback: reserved, and refused immediately rather than
	// dropped, so this is fast everywhere rather than dependent on a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := Open(ctx, "redis://127.0.0.1:1"); err == nil {
		t.Fatal("want an error for an unreachable server")
	}
}

// Against a real server, when one is offered. Skipped rather than failed without
// it, the way the orchestrator's database tests are: CI has no Redis, and a test
// that cannot run is not a test that failed.
func TestOpenAgainstARealRedis(t *testing.T) {
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL is not set")
	}

	client, err := Open(context.Background(), url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
