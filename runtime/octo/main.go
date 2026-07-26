package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/juancavallotti/octo/runtime/connectors/cron"
	_ "github.com/juancavallotti/octo/runtime/connectors/database" // registers the "database" connector and "sql" block
	_ "github.com/juancavallotti/octo/runtime/connectors/events"   // registers the "events" connector + source and the "publish-event" block
	_ "github.com/juancavallotti/octo/runtime/connectors/http"
	_ "github.com/juancavallotti/octo/runtime/connectors/httpclient"    // registers the "http-client" connector and "rest" block
	_ "github.com/juancavallotti/octo/runtime/connectors/llm/aiblocks"  // registers the "ai-mapping" block
	_ "github.com/juancavallotti/octo/runtime/connectors/llm/anthropic" // registers the "llm-anthropic" connector
	_ "github.com/juancavallotti/octo/runtime/connectors/llm/gemini"    // registers the "llm-gemini" connector
	_ "github.com/juancavallotti/octo/runtime/connectors/llm/openai"    // registers the "llm-openai" connector
	_ "github.com/juancavallotti/octo/runtime/connectors/logger"        // registers the "logger" connector and "log" block
	_ "github.com/juancavallotti/octo/runtime/connectors/notion"        // registers the "notion" connector and its blocks
	_ "github.com/juancavallotti/octo/runtime/connectors/queue"         // registers the "queue" connector + source and the "queue-dispatch" block
	_ "github.com/juancavallotti/octo/runtime/connectors/slack"         // registers the "slack" connector and its blocks
	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services"
	// The active runtime-services provider is selected at build time by tag:
	// standalone by default, k8s with -tags k8s. See providers_*.go. Hosted
	// runtime services are gated the same way; see hosted_*.go.
)

func main() {
	level, levelErr := core.ParseLevel(os.Getenv("LOG_LEVEL"))
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	if levelErr != nil {
		slog.Warn("invalid LOG_LEVEL, defaulting to info", "error", levelErr)
	}

	if err := run(os.Args[1:]); err != nil {
		slog.Error("cli stopped with error", "error", err)
		os.Exit(1)
	}
}

// teeDefaultLoggerToSink re-installs the process default logger so it also writes
// to svc's central log sink, when the active runtime-services module ships logs
// (the k8s module). The standalone module ships nothing, so the default logger is
// left as the stderr handler installed in main. Called once after the services are
// built; the stderr base keeps applying LOG_LEVEL while the sink captures records
// for the central aggregator.
func teeDefaultLoggerToSink(svc core.RuntimeServices) {
	shipper, ok := svc.(core.LogShipper)
	if !ok {
		return
	}
	sink := shipper.LogSink()
	if sink == nil {
		return
	}
	level, _ := core.ParseLevel(os.Getenv("LOG_LEVEL"))
	base := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(core.TeeHandler(base, sink)))
}

// run dispatches to a subcommand. The default (no subcommand, or a leading flag)
// is "run", so `cli -config x.yaml` keeps working.
// usage is the top-level help page, printed for `octo`, `octo --help`, and a
// subcommand's --help.
const usage = `octo — run and invoke octo integration flows

Usage:
  octo [run] --config <path> [--watch]                       Start connectors and flows (default)
  octo invoke --config <path> --flow <name> [--data <json>] [--vars <json>]
                                                             Call one flow and print its result
  octo eval --expr <cel> [--data <json>]                     Evaluate a CEL expression and print the result
  octo schema [--out <path>]                                 Print the editor capability schema as JSON
  octo version                                               Print the version and build date
  octo --help                                                Show this help

Run flags:
  --config <path>   path to the runtime config (file or directory)
  --watch           reload the config when it changes

Invoke flags:
  --config <path>    path to the runtime config (file or directory)
  --flow <name>      name of the flow to invoke
  --data <json>      JSON request body (reads stdin when omitted)
  --content-type <mime>
                     treat --data as a raw body of this MIME type instead of JSON
                     (e.g. application/xml), for flows fed by a raw-body source
  --vars <json>      JSON object seeding the message variables
  --timeout <dur>    max time to wait for the flow (default 30s)
  --break-at <addr>  run until this block, then print the message and stop
  --spies <addrs>    comma-separated blocks to record every message that crosses them
  --mocks <json>     blocks to answer for instead of running them (see below)
  --run-debug-config <path>
                     a file holding the whole run: input, breakAt, spies, mocks
  --envelope         always print the outcome envelope, and report a flow failure in
                     it rather than exiting non-zero

  Sources are not started, so nothing populates the message for you. --vars is how
  you seed what a source normally would: the http source copies request headers into
  the message variables, so a flow that reads vars needs them supplied here.

The outcome envelope (--envelope):
  A plain invoke says what happened three different ways: a flow that completed prints
  its message, one that DROPPED the message prints nothing at all, and one that FAILED
  prints nothing and exits 1 with the error in a log line. That is fine for a person
  reading a terminal and unusable for a program: stdout cannot tell a drop from a
  result, and a failure has to be scraped out of the logs.

  --envelope prints one JSON object that says all three:

    {"result":{"event_id":"…","variables":{…},"body":{…}}}   the flow completed
    {"dropped":true}                                          a filter dropped it
    {"error":"block \"charge\": card declined"}               the flow failed

  A flow that fails exits 0 under --envelope: the flow failing is the answer to the
  question that was asked, not a failure of the CLI. A run that could not START — an
  unresolvable block address, a broken config — is a bad request and still exits
  non-zero. This is what "dolphin" reads.

Debug config (--run-debug-config):
  A whole debugging run in one file — YAML, or JSON, which is a subset of it. Check it
  in next to the flow it exercises and the session is reproducible, instead of being a
  page of flags someone has to retype.

    input:
      data: { amount: 250 }
      vars: { x-api-key: dev-key }
    breakAt: orders.charge
    spies:
      - orders.validate
      - orders.fanout[audit].log-it
    mocks:
      orders.charge:
        cases:
          - when: 'body.amount > 100'
            error: card declined
        default:
          drop: true

  A flag overrides the section it corresponds to, whole: --spies replaces the file's
  spy list rather than adding to it, so a flag always says exactly what the run does.
  For the body the order is --data, then the file's input.data, then piped stdin — the
  file comes before stdin so a fully-described run never sits waiting on a pipe.

Mocks (--mocks):
  A JSON object keyed by block address. Each block lists cases tried in order: a CEL
  "when" over the message the block received, and what the block should have returned
  — a "body" (with optional "vars"), an "error", or "drop". An optional "default"
  catches the rest; without one, a message that matches no case fails the block.

    --mocks '{"orders.charge":{
       "cases":[{"when":"body.amount > 100","error":"card declined"},
                {"when":"true","body":{"ok":true},"vars":{"status":200}}],
       "default":{"drop":true}}}'

  The block is replaced, not wrapped, so its real work — the HTTP call, the LLM, the
  database write — never happens. Mocking a composite (a fork, an ai-agent) cuts out
  everything inside it.

Block addresses (--break-at, --spies, --mocks):
  Address a block as <flow>.<block>, descending into a composite with a bracket
  naming the branch to follow:

    orders.charge                      a block in the flow's own chain
    orders.checkHeader[else].api-call  the "else" branch of an if
    orders.loop[body].transform        a foreach/enrich/cache-scope body
    orders.guard[error].notify         a handle-errors error path
    orders.fanout[audit].log-it        a fork branch, by its name
    orders.pick[vip].comp              a switch case (or ai-router route, ai-agent tool)
    orders[error].notify               the flow's own error chain

  A block is named by its "name", else its type, else its ref; name it if two of
  a kind share a chain. The output is a JSON envelope: {"reached":true,"message":
  {...}} when the flow got there, {"reached":false} when it never did.

Eval flags:
  --expr <cel>       CEL expression to evaluate (required)
  --data <json>      JSON object bound to body (reads stdin when omitted)
  --vars <json>      JSON object bound to vars
  --env <json>       JSON object bound to env

Schema flags:
  --kind <kind>      which schema: capabilities (default) or debug-config
  --format <fmt>     json (default) or yaml
  --out <path>       write it to a file instead of stdout

  "octo schema --kind debug-config" prints the JSON Schema of a --run-debug-config
  file, so an editor can complete it and a validator can check it.`

// dashNote closes the help page. It is separate from usage so hosted-service
// sections land above it: it is a note about every flag on the page, so it has to
// come after the last one.
const dashNote = `Flags accept one or two dashes (--config or -config).`

// usageText is the help page as printed: the static page above, then a section
// from every hosted runtime service compiled into this binary, then the closing
// note. Hosted services bring their own `octo run` flags — they have no YAML to
// configure them — so their documentation has to come from them; a flag that
// exists but is absent from --help may as well not exist.
func usageText() string {
	hosted := services.HostedUsage()
	if hosted == "" {
		return usage + "\n\n" + dashNote
	}
	return usage + "\n\n" + hosted + "\n\n" + dashNote
}

func run(args []string) error {
	// Handle top-level help and version before subcommand dispatch: the run/invoke
	// flagsets do not define them, so `octo --help` / `octo --version` must be
	// intercepted here. Go's flag package treats -x and --x alike, so honor both
	// dash forms.
	if len(args) == 0 {
		fmt.Println(usageText())
		return nil
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Println(usageText())
		return nil
	case "-version", "--version", "version":
		fmt.Println(versionLine())
		return nil
	}

	cmd := "run"
	if !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "run":
		return runCommand(args)
	case "invoke":
		return invokeCommand(args)
	case "eval":
		return evalCommand(args)
	case "schema", "capabilities":
		return schemaCommand(args)
	default:
		return fmt.Errorf("unknown command %q (expected \"run\", \"invoke\", \"eval\", \"schema\", or \"version\")", cmd)
	}
}

// configDir returns the directory a config path is rooted at — the directory
// itself when path is a config directory, else the config file's parent. It is
// the resource root handed to the runtime-services provider.
func configDir(configPath string) string {
	if info, err := os.Stat(configPath); err == nil && !info.IsDir() {
		return filepath.Dir(configPath)
	}
	return configPath
}
