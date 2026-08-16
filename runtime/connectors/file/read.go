// This file provides the "file-read" leaf block: it reads a file from the
// connector's root and puts the contents on the message as raw content, so a
// non-JSON file (YAML, CSV, Markdown, an image) survives without being forced
// through a JSON decode. The path is a CEL expression, so a flow exposed as an
// agent tool can be handed the file name to read at call time.
package file

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const readBlock = connectorType + "-read"

// encodingText stores the bytes on the message as they are; encodingBase64
// stores them base64-encoded. See readSettings.Encoding for which to pick.
const (
	encodingText   = "text"
	encodingBase64 = "base64"
)

// defaultContentType is recorded when the file's extension names no MIME type.
const defaultContentType = "application/octet-stream"

// contentTypesByExt pins the types for the formats a flow actually reads, ahead
// of the standard library's lookup.
//
// mime.TypeByExtension alone will not do for these. Its built-in table does not
// carry .yaml at all, and what it does carry is extended from the host's
// mime.types files — so the same flow would record a different content type on a
// laptop than in a container. The formats worth being deterministic about are few
// and stable, so they are named here; the standard lookup still handles the long
// tail, where a host-specific answer beats no answer.
var contentTypesByExt = map[string]string{
	".yaml": "application/yaml",
	".yml":  "application/yaml",
	".toml": "application/toml",
	".env":  "text/plain; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
	".csv":  "text/csv; charset=utf-8",
	".tsv":  "text/tab-separated-values; charset=utf-8",
	".json": "application/json",
}

func registerRead() {
	core.MustRegisterBlock(readBlock, newRead)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        readBlock,
		Label:       "File Read",
		Category:    core.CategoryProcessor,
		Icon:        "HardDriveDownload",
		Description: "Read a file from a file connector's root into the message.",
		Config:      reflect.TypeFor[readSettings](),
	})
}

// readSettings is the file-read block's typed configuration.
type readSettings struct {
	// Name of the file connector whose root the path is resolved under.
	Connector string `json:"connector" octo:"label=File connector,ref=connector:file,required"`
	// CEL expression evaluated to the path to read, relative to the connector's root.
	Path string `json:"path" octo:"label=Path,type=cel,required"`
	// How the file's bytes are carried. Text stores them as they are, which suits
	// any text file. Base64 encodes them, which is what a binary file needs to
	// survive a queue hop or a trace.
	Encoding string `json:"encoding" octo:"label=Encoding,type=enum,enum=text|base64,default=text"`
	// MIME type recorded on the raw body. Defaults to the one the file extension
	// names, or application/octet-stream.
	ContentType string `json:"contentType" octo:"label=Content type"`
	// Variable to store the contents in. Empty replaces the message body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

// readProcessor reads a file per message and puts its contents on the message.
type readProcessor struct {
	connector   *Connector
	path        *expr.Program
	encoding    string
	contentType string
	resultVar   string
	env         map[string]any
}

// newRead builds a file-read processor, resolving the connector and compiling the
// path expression once so a bad reference or expression fails at startup rather
// than on the first message.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newRead(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg readSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Path == "" {
		return nil, fmt.Errorf("%s requires a path expression", readBlock)
	}
	encoding, err := parseEncoding(readBlock, cfg.Encoding)
	if err != nil {
		return nil, err
	}
	connector, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, err
	}
	path, err := expr.CompileMessage(deps.Resources, cfg.Path)
	if err != nil {
		return nil, err
	}
	return &readProcessor{
		connector:   connector,
		path:        path,
		encoding:    encoding,
		contentType: cfg.ContentType,
		resultVar:   cfg.ResultVar,
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// parseEncoding validates an encoding setting, defaulting to text.
func parseEncoding(block, encoding string) (string, error) {
	switch encoding {
	case "", encodingText:
		return encodingText, nil
	case encodingBase64:
		return encodingBase64, nil
	default:
		return "", fmt.Errorf("%s encoding must be %q or %q, got %q",
			block, encodingText, encodingBase64, encoding)
	}
}

// Process reads the file the path expression names and puts its contents on the
// message, either as the raw-content body or in a variable.
func (p *readProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	path, err := p.path.EvalString(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("%s path: %w", readBlock, err)
	}
	data, err := p.connector.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", readBlock, err)
	}

	contents := string(data)
	if p.encoding == encodingBase64 {
		contents = base64.StdEncoding.EncodeToString(data)
	}
	if p.resultVar != "" {
		msg.Variables.Set(p.resultVar, contents)
		return msg, nil
	}
	msg.SetRawBody(p.contentTypeFor(path), contents)
	return msg, nil
}

// contentTypeFor is the configured content type, or the one the file extension
// names, or a final fallback.
func (p *readProcessor) contentTypeFor(path string) string {
	if p.contentType != "" {
		return p.contentType
	}
	ext := strings.ToLower(filepath.Ext(path))
	if pinned, ok := contentTypesByExt[ext]; ok {
		return pinned
	}
	if byExt := mime.TypeByExtension(ext); byExt != "" {
		return byExt
	}
	return defaultContentType
}
