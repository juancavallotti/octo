// This file is the type bridge between BSON and the message. Everything the
// mongodb blocks read or write crosses it.
//
// BSON has types JSON does not: ObjectId, dates, Decimal128, binary, and true
// 64-bit integers. A message body, by contract, is decoded JSON — see the
// commentary on types.copyBody. So something has to give, and the choice made
// here is MongoDB Extended JSON v2 in both directions rather than a lossy
// flattening of its own.
//
// The reason is symmetry. Under a flattening convention (ObjectId to a hex
// string, dates to RFC 3339) the document a find returns cannot be handed back
// to a filter: the string no longer matches the ObjectId it came from, and
// nothing in the body says so. Under extended JSON it round-trips, because
// {"$oid": "..."} is the shape both sides agree on. It is also a documented
// MongoDB format rather than a convention this package invented, so what a flow
// author already knows about mongosh and Compass output transfers.
//
// Relaxed is the default because canonical turns every number into an object,
// which is a heavy price for a precision problem most collections do not have.
// Input is always parsed leniently, so a filter written either way is accepted
// no matter how results are rendered.
package mongodb

import (
	"bytes"
	"encoding/json"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// decodeValue turns an evaluated CEL value — which is always JSON-native, see
// expr.Program.Eval — into the BSON the driver takes. The JSON encoding in
// between is not a detour: it is what lets an author write {"$oid": body.id} or
// {"$date": "..."} in a filter and have the driver's extended JSON parser build
// the real BSON type from it.
//
// canonicalOnly is false so both the canonical and relaxed spellings are
// accepted regardless of the connector's output mode. A filter is never
// rejected for being written in the "wrong" one.
func decodeValue(label string, value any) (bson.Raw, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", label, err)
	}
	var doc bson.Raw
	if err := bson.UnmarshalExtJSON(raw, false, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return doc, nil
}

// decodeDocument evaluates a CEL result that must be a single document, so a
// list or a scalar is reported against the field that produced it rather than
// as a driver error further down.
func decodeDocument(label string, value any) (bson.Raw, error) {
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("%s must evaluate to an object, got %T", label, value)
	}
	return decodeValue(label, value)
}

// decodePipeline evaluates a CEL result that must be a list of stage documents.
// The driver would take a bare document too, but an aggregation pipeline is a
// sequence by definition and a one-stage pipeline written without its brackets
// is a mistake worth naming.
func decodePipeline(label string, value any) ([]bson.Raw, error) {
	stages, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must evaluate to a list of stages, got %T", label, value)
	}
	out := make([]bson.Raw, 0, len(stages))
	for i, stage := range stages {
		doc, err := decodeDocument(fmt.Sprintf("%s[%d]", label, i), stage)
		if err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, nil
}

// encodeDocument renders one document as extended JSON, ready for
// types.Message.SetBodyJSON.
func encodeDocument(doc bson.Raw, canonical bool) ([]byte, error) {
	raw, err := bson.MarshalExtJSON(doc, canonical, false)
	if err != nil {
		return nil, fmt.Errorf("encode document: %w", err)
	}
	return raw, nil
}

// encodeDocuments renders documents as a JSON array of extended JSON documents.
// It assembles the array by hand because a top-level BSON array is not a
// document, so this cannot be one MarshalExtJSON call.
//
// An empty result is "[]", never "null": a find that matched nothing is an
// empty list, and a flow iterating the body should get zero iterations rather
// than a type error.
func encodeDocuments(docs []bson.Raw, canonical bool) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, doc := range docs {
		if i > 0 {
			buf.WriteByte(',')
		}
		raw, err := encodeDocument(doc, canonical)
		if err != nil {
			return nil, err
		}
		buf.Write(raw)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// encodeValue renders a single BSON value — an inserted _id, an upserted _id —
// as extended JSON. The driver has no marshaler for a bare value, so it is
// wrapped in a one-field document and unwrapped again after encoding.
func encodeValue(value any, canonical bool) (json.RawMessage, error) {
	raw, err := bson.MarshalExtJSON(bson.D{{Key: valueKey, Value: value}}, canonical, false)
	if err != nil {
		return nil, fmt.Errorf("encode value: %w", err)
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("encode value: %w", err)
	}
	return wrapper[valueKey], nil
}

// valueKey is the throwaway field name encodeValue wraps a bare value in.
const valueKey = "v"
