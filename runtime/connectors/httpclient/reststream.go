// This file provides the "rest-stream" block: a request whose answer is read a
// line at a time as it arrives, running a sub-flow on each line.
//
// It is the shape for an upstream that never finishes answering in the ordinary
// sense — a log tail, an NDJSON feed, another server's event stream — which rest
// and rest-dynamic cannot express, because both read the whole response before
// the flow sees any of it.
//
// What a flow does with each line is the flow's business. Relaying lines to a
// caller as server-sent events is an sse-event in the body; the block itself
// knows nothing about who is listening, so it stays inside this connector.
package httpclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	// defaultStreamVar is the variable each line is bound to.
	defaultStreamVar = "line"
	// defaultMaxLineBytes bounds one line, not the stream: the stream has no size,
	// but a line is held in memory whole.
	defaultMaxLineBytes = 1 << 20
	// initialLineBuffer is what the scanner starts with, growing toward
	// maxLineBytes only for a line that needs it.
	initialLineBuffer = 64 << 10
)

func registerRESTStream() {
	core.MustRegisterBlock("rest-stream", newRESTStream)

	// Group ("Integration") and Icon ("Globe") are inherited from the package
	// ExtensionMeta registered in httpclient.go.
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "rest-stream",
		Label:    "Streaming REST Call",
		Category: core.CategoryControlFlow,
		Description: "Make an HTTP request through an http-client connector and run a flow " +
			"on each line of the response as it arrives.",
		Config: reflect.TypeFor[restStreamSettings](),
	})
}

// restStreamSettings is the rest-stream block's typed configuration: the request
// rest-dynamic renders, plus how the answer is read.
type restStreamSettings struct {
	// Name of the http-client connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:http-client"`
	// CEL expression for the HTTP method.
	Method string `json:"method" octo:"label=Method,type=cel,required"`
	// CEL expression for the path, resolved against the connector's base URL.
	Path string `json:"path" octo:"label=Path,type=cel,required"`
	// CEL expression evaluating to a map of query parameters.
	Query string `json:"query" octo:"label=Query,type=cel"`
	// CEL expression evaluating to a map of request headers.
	Headers string `json:"headers" octo:"label=Headers,type=cel"`
	// CEL expression for the request body.
	Body string `json:"requestBody" octo:"label=Request body,type=cel"`
	// Methods this block may issue. Empty allows any of the standard set.
	AllowMethods []string `json:"allowMethods" octo:"label=Allowed methods,type=string-list"`
	// Path prefix this block may call under. Empty allows the whole base URL.
	PathPrefix string `json:"pathPrefix" octo:"label=Path prefix"`
	// Turn a 400+ status into a flow error. When false the error body is folded
	// into the message, as rest does, and no line is read.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
	// Variable to store the response status code in.
	StatusVar string `json:"statusVar" octo:"label=Status variable,default=statusCode"`
	// Variable each line is bound to while the flow runs on it.
	As string `json:"as" octo:"label=As,default=line"`
	// Longest line accepted, in bytes. A longer one ends the stream with an error.
	MaxLineBytes int `json:"maxLineBytes" octo:"label=Max line bytes,default=1048576"`
	// How long the stream may stay open. Empty leaves it open until the upstream
	// ends it or the flow stops.
	MaxDuration duration `json:"maxDuration" octo:"label=Max duration,type=string"`
	// Flow run once per line.
	Each *types.FlowConfig `json:"body" octo:"label=Body,type=flow,required"`
}

// streamProcessor renders a request as rest-dynamic does and runs body on each
// line of a successful answer, threading the message through like foreach.
type streamProcessor struct {
	request      *dynamicProcessor
	body         core.MessageProcessor
	as           string
	maxLineBytes int
	maxDuration  time.Duration
}

// newRESTStream builds the processor. The request half is rest-dynamic's own,
// so the two bound what a rendered request may be in exactly the same way.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newRESTStream(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg restStreamSettings
	if err := raw.DecodeStrict(&cfg); err != nil {
		return nil, err
	}
	if cfg.Each == nil {
		return nil, errors.New("rest-stream block requires a body flow")
	}
	if cfg.MaxLineBytes < 0 {
		return nil, errors.New("rest-stream block: maxLineBytes cannot be negative")
	}
	if cfg.MaxDuration < 0 {
		return nil, errors.New("rest-stream block: maxDuration cannot be negative")
	}

	request, err := newDynamicProcessor("rest-stream", restDynamicSettings{
		Connector:    cfg.Connector,
		Method:       cfg.Method,
		Path:         cfg.Path,
		Query:        cfg.Query,
		Headers:      cfg.Headers,
		Body:         cfg.Body,
		AllowMethods: cfg.AllowMethods,
		PathPrefix:   cfg.PathPrefix,
		FailOnError:  cfg.FailOnError,
		StatusVar:    cfg.StatusVar,
	}, deps)
	if err != nil {
		return nil, err
	}

	flows, err := core.SubFlowsOf(deps)
	if err != nil {
		return nil, err
	}
	body, err := flows.Branch(core.BranchBody, *cfg.Each)
	if err != nil {
		return nil, fmt.Errorf("rest-stream body: %w", err)
	}

	ret := &streamProcessor{
		request:      request,
		body:         body,
		as:           cfg.As,
		maxLineBytes: cfg.MaxLineBytes,
		maxDuration:  time.Duration(cfg.MaxDuration),
	}
	if ret.as == "" {
		ret.as = defaultStreamVar
	}
	if ret.maxLineBytes == 0 {
		ret.maxLineBytes = defaultMaxLineBytes
	}
	return ret, nil
}

// Process opens the stream and runs the body on each line until the upstream
// ends it, maxDuration passes, or the body stops or drops the message.
//
// Reaching maxDuration is an ordinary end, not a failure: it is the bound the
// flow asked for. The context the flow was called with ending is a failure,
// since nothing asked for that.
func (p *streamProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	streamCtx, cancel := p.bound(ctx)
	defer cancel()

	req, target, err := p.request.render(streamCtx, msg)
	if err != nil {
		return nil, err
	}
	resp, err := p.request.conn.Stream(req)
	if err != nil {
		return nil, fmt.Errorf("rest-stream request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	msg.Variables.Set(p.request.statusVar, resp.StatusCode)

	// A refusal is a short answer and not a stream, so it is read and folded the
	// way rest-dynamic would fold it, bounded by the connector's cap.
	if resp.StatusCode >= http.StatusBadRequest {
		return p.refused(resp, req, target, msg)
	}

	ret, err := p.eachLine(streamCtx, resp.Body, msg)
	if err == nil {
		return ret, nil
	}
	// maxDuration ran out, which is the end of the stream the flow asked for. It
	// is decided by the stream's own deadline rather than by the error, which may
	// be a timeout of something inside the body that has nothing to do with it.
	if errors.Is(streamCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return ret, nil
	}
	return nil, err
}

// bound applies maxDuration to the stream's context, when there is one.
func (p *streamProcessor) bound(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.maxDuration > 0 {
		return context.WithTimeout(ctx, p.maxDuration)
	}
	return context.WithCancel(ctx)
}

// refused handles a 4xx or 5xx answer: an error when failOnError, otherwise its
// body folded into the message with no line read.
func (p *streamProcessor) refused(
	resp *http.Response, req *http.Request, target string, msg *types.Message,
) (*types.Message, error) {
	body, err := io.ReadAll(newLimitedBody(resp.Body, p.request.conn.maxBytes))
	if err != nil {
		return nil, fmt.Errorf("rest-stream read response: %w", err)
	}
	if p.request.failOnError {
		return nil, fmt.Errorf("rest-stream request to %s returned %d: %s",
			requestedURL(req, target), resp.StatusCode, snippet(body))
	}
	if err := foldResponse(msg, body, resp.Header.Get("Content-Type")); err != nil {
		return nil, err
	}
	return msg, nil
}

// eachLine runs the body once per line, binding it to the loop variable and
// restoring whatever that variable held before however the stream ends.
//
// On an error it still returns the latest message the body produced, so a
// stream its deadline cut short keeps what the body did up to then.
//
// A stop raised in the body — an sse-event finding its caller gone is the one
// that matters — ends the stream on that line, which is how a relay stops
// reading an upstream nobody is listening to any more.
func (p *streamProcessor) eachLine(ctx context.Context, r io.Reader, msg *types.Message) (*types.Message, error) {
	prev, had := msg.Variables[p.as]
	defer func() {
		// msg is the latest message the body returned, which is the one that
		// carries the variable on.
		if msg == nil {
			return
		}
		if had {
			msg.Variables.Set(p.as, prev)
		} else {
			delete(msg.Variables, p.as)
		}
	}()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, min(p.maxLineBytes, initialLineBuffer)), p.maxLineBytes)
	for scanner.Scan() {
		msg.Variables.Set(p.as, strings.TrimSuffix(scanner.Text(), "\r"))
		out, err := p.body.Process(ctx, msg)
		if err != nil {
			return msg, err
		}
		if out == nil {
			return nil, nil
		}
		msg = out
		if msg.StopRequested() {
			return msg, nil
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return msg, fmt.Errorf("rest-stream: a line is longer than maxLineBytes (%d)", p.maxLineBytes)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return msg, fmt.Errorf("rest-stream read: %w", ctxErr)
		}
		return msg, fmt.Errorf("rest-stream read: %w", err)
	}
	return msg, nil
}
