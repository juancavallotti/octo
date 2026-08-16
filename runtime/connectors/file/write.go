// This file provides the "file-write" leaf block: it writes a file into the
// connector's root and passes the message through unchanged, so a write can sit
// mid-flow like a log block. The path is a CEL expression for the same reason
// file-read's is — a flow exposed as an agent tool is told where to write at
// call time.
package file

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const writeBlock = connectorType + "-write"

// bytesWrittenKey and pathKey are the fields of the receipt resultVar holds.
const (
	bytesWrittenKey = "bytes"
	pathKey         = "path"
)

func registerWrite() {
	core.MustRegisterBlock(writeBlock, newWrite)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        writeBlock,
		Label:       "File Write",
		Category:    core.CategoryProcessor,
		Icon:        "HardDriveUpload",
		Description: "Write a file into a file connector's root, passing the message through.",
		Config:      reflect.TypeFor[writeSettings](),
	})
}

// writeSettings is the file-write block's typed configuration.
type writeSettings struct {
	// Name of the file connector whose root the path is resolved under.
	Connector string `json:"connector" octo:"label=File connector,ref=connector:file,required"`
	// CEL expression evaluated to the path to write, relative to the connector's root.
	Path string `json:"path" octo:"label=Path,type=cel,required"`
	// CEL expression for the contents. Empty writes the message body: the raw
	// content as-is when the message carries some, otherwise its JSON encoding.
	Content string `json:"content" octo:"label=Content,type=cel"`
	// How to read the contents before writing. Text writes them as they are;
	// base64 decodes them first, which is what restores a binary file read with
	// the same setting.
	Encoding string `json:"encoding" octo:"label=Encoding,type=enum,enum=text|base64,default=text"`
	// Variable to store a {path, bytes} receipt in. Empty stores nothing.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

// writeProcessor writes a file per message and forwards the message unchanged.
// content is nil when no expression is configured, in which case the message
// body is written.
type writeProcessor struct {
	connector *Connector
	path      *expr.Program
	content   *expr.Program
	encoding  string
	resultVar string
	env       map[string]any
}

// newWrite builds a file-write processor, resolving the connector and compiling
// both expressions once so a bad reference or expression fails at startup.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newWrite(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg writeSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Path == "" {
		return nil, fmt.Errorf("%s requires a path expression", writeBlock)
	}
	encoding, err := parseEncoding(writeBlock, cfg.Encoding)
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
	processor := &writeProcessor{
		connector: connector,
		path:      path,
		encoding:  encoding,
		resultVar: cfg.ResultVar,
		env:       expr.EnvActivation(deps.Env),
	}
	if cfg.Content != "" {
		if processor.content, err = expr.CompileMessage(deps.Resources, cfg.Content); err != nil {
			return nil, err
		}
	}
	return processor, nil
}

// Process writes the file and returns the message unchanged, so the flow carries
// on with whatever it was already holding.
func (p *writeProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	path, err := p.path.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("%s path: %w", writeBlock, err)
	}
	contents, err := p.contents(msg, activation)
	if err != nil {
		return nil, err
	}
	data := []byte(contents)
	if p.encoding == encodingBase64 {
		if data, err = base64.StdEncoding.DecodeString(contents); err != nil {
			return nil, fmt.Errorf("%s content is not base64: %w", writeBlock, err)
		}
	}
	if err := p.connector.WriteFile(path, data); err != nil {
		return nil, fmt.Errorf("%s: %w", writeBlock, err)
	}
	if p.resultVar != "" {
		msg.Variables.Set(p.resultVar, map[string]any{
			pathKey:         path,
			bytesWrittenKey: len(data),
		})
	}
	return msg, nil
}

// contents resolves what to write: the content expression when one is
// configured, else the message's raw content, else its body as JSON. Falling
// back through raw content matters — JSON-encoding a body that is already exact
// text would write the quotes and escapes too.
func (p *writeProcessor) contents(msg *types.Message, activation map[string]any) (string, error) {
	if p.content != nil {
		value, err := p.content.EvalString(activation)
		if err != nil {
			return "", fmt.Errorf("%s content: %w", writeBlock, err)
		}
		return value, nil
	}
	if _, rawData, ok := msg.RawBody(); ok {
		return rawData, nil
	}
	body, err := msg.BodyJSON()
	if err != nil {
		return "", fmt.Errorf("%s body: %w", writeBlock, err)
	}
	return string(body), nil
}
