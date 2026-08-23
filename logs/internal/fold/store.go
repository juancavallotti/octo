package fold

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Where the fold state lives.
//
// One hash per open run, a second beside it for the run's text keyed by sequence,
// and one sorted set of every open run scored by when it is due to close.
const (
	keyPrefix  = "octo:fold:"
	chunkSuf   = ":chunks"
	pendingSuf = ":pending"
	openZSet   = "octo:fold:open"
)

// Hash fields. Everything Redis holds is a string, so the numbers are decimal and
// the flags are "1" or "".
const (
	fShape     = "shape"
	fCount     = "count"
	fLastSeq   = "lastSeq"
	fLastEnd   = "lastEnd"
	fBytes     = "bytes"
	fTruncated = "truncated"
	fMergeable = "mergeable"
	fErr       = "err"
	fDropped   = "dropped"
	fFirst     = "first"
)

// Store holds open runs in Redis, so a fold survives its records being spread
// across replicas.
//
// That spread is not hypothetical: the aggregator's consumers join a NATS queue
// group, so consecutive records of one trace go to whichever replica is free. An
// in-process fold would work at one replica and silently stop working at two,
// which is the worst way for a thing like this to fail.
//
// The scripts below touch keys they were not passed, which rules out Redis
// Cluster — the expiry sweep reads a hash it found in the sorted set, and no
// caller could have named it in advance. The bundled server is a single node, and
// externalRedis.url is documented as needing to be one too.
type Store struct {
	client *redis.Client

	// window is how long a run stays open with nothing arriving. It is the only
	// thing that ends a run of one shape that simply stopped — the flow moving on
	// to another block starts a different key rather than closing this one, and
	// this is what collects it afterwards.
	window time.Duration
	// ttl is the hard expiry on every key, so a run abandoned by a replica that
	// died cannot outlive its usefulness. Well above window: it is a backstop for
	// nothing ever sweeping again, not a second deadline.
	ttl time.Duration
	// maxBytes caps a run's merged text.
	maxBytes int
	// minRun is the shortest run worth rewriting as a fold.
	minRun int

	append *redis.Script
	expire *redis.Script
}

// NewStore returns a Store over client.
func NewStore(client *redis.Client, window, ttl time.Duration, maxBytes, minRun int) *Store {
	return &Store{
		client:   client,
		window:   window,
		ttl:      ttl,
		maxBytes: maxBytes,
		minRun:   minRun,
		append:   redis.NewScript(appendScript),
		expire:   redis.NewScript(expireScript),
	}
}

// Append adds r to its run and returns whatever the run it displaced closed to.
//
// A run closes here for one reason: the body's shape changed, which means what was
// streaming is not what is streaming now. That is the boundary between an agent's
// thinking and its answer, and folding across it would produce one record claiming
// to be both.
//
// **The caller must store what comes back and must not store r itself.** A record
// handed to Append is held rather than written, so this is the only path by which
// it reaches the database — either folded into the record that stands for its run,
// or handed back verbatim when the run turned out to be too short to fold.
func (s *Store) Append(ctx context.Context, r Record, now time.Time) ([]Record, error) {
	key := KeyOf(r)
	shape, text, mergeable := split(r.Record.Body)

	first, err := json.Marshal(encodable(r))
	if err != nil {
		return nil, fmt.Errorf("fold: encode record: %w", err)
	}

	hash := keyPrefix + key.String()
	res, err := s.append.Run(ctx, s.client,
		[]string{hash, hash + chunkSuf, hash + pendingSuf, openZSet},
		shape,
		text,
		strconv.FormatInt(r.Record.Seq, 10),
		strconv.FormatInt(r.Record.Time.UnixMilli(), 10),
		r.Record.Err,
		boolArg(r.Record.Dropped || r.Record.Truncated),
		boolArg(mergeable),
		string(first),
		strconv.FormatInt(now.Add(s.window).UnixMilli(), 10),
		strconv.Itoa(int(s.ttl.Seconds())),
		strconv.Itoa(s.maxBytes),
		strconv.Itoa(s.minRun),
	).Result()
	if err != nil {
		// redis.Nil is the script returning nothing, which is the common case: the
		// record extended a run and displaced nothing.
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("fold: append: %w", err)
	}
	return s.decodeOne(res)
}

// Expire closes every run whose window has passed, at most limit of them.
//
// The pop is one script for a reason: finding a due run and removing it have to
// happen together, or two replicas sweeping at the same moment would both find it
// and both write it. Atomicity here is what makes a folded record appear exactly
// once.
//
// A replica that dies between popping and storing loses that run — the same
// at-most-once contract the rest of this pipeline ships with, and the reason the
// batch below drops rather than retries a failed write.
func (s *Store) Expire(ctx context.Context, now time.Time, limit int) ([]Record, error) {
	res, err := s.expire.Run(ctx, s.client,
		[]string{openZSet},
		strconv.FormatInt(now.UnixMilli(), 10),
		strconv.Itoa(limit),
	).Result()
	if err != nil {
		return nil, fmt.Errorf("fold: expire: %w", err)
	}

	raw, ok := res.([]any)
	if !ok {
		return nil, nil
	}
	var out []Record
	for _, item := range raw {
		recs, err := s.decodeOne(item)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	return out, nil
}

// decodeOne turns one popped run into the records it owes back: one folded
// record, or — when the run was too short to be worth rewriting — the records it
// was holding, unchanged.
//
// Both cases have to produce rows. Nothing else stored them.
func (s *Store) decodeOne(v any) ([]Record, error) {
	parts, ok := v.([]any)
	if !ok || len(parts) != 3 {
		return nil, nil
	}
	fields := toMap(parts[0])
	if len(fields) == 0 {
		return nil, nil
	}

	open, err := decodeOpen(fields, toMap(parts[1]))
	if err != nil {
		return nil, err
	}
	if rec, folded := open.Close(s.minRun); folded {
		return []Record{rec}, nil
	}
	return decodePending(toMap(parts[2]))
}

// decodePending rebuilds the records a short run was holding, in sequence order
// so a batch lands them the way they happened.
//
// An unreadable one is skipped rather than failing the sweep: it is one row, and
// abandoning the rest of the pop would lose every other record in it — the sweep
// has already removed them from Redis and there is nothing to retry against.
func decodePending(pending map[string]string) ([]Record, error) {
	seqs := make([]int, 0, len(pending))
	for seq := range pending {
		seqs = append(seqs, atoi(seq))
	}
	sort.Ints(seqs)

	out := make([]Record, 0, len(seqs))
	for _, seq := range seqs {
		rec, err := decodeRecord(pending[strconv.Itoa(seq)])
		if err != nil {
			slog.Warn("fold: drop unreadable held record", "seq", seq, "err", err)
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// decodeRecord reads a record back out of the store, undoing what the round trip
// did to its absent payloads.
//
// A nil json.RawMessage marshals to the four bytes `null` and unmarshals back as
// those four bytes rather than as nil, and the difference is load-bearing all the
// way to the column: the store writes a nil body as SQL NULL, meaning the runtime
// captured nothing, and `null` as the JSON value null, meaning it captured a null.
// A record that passed through a fold must not acquire the second by having been
// held.
func decodeRecord(encoded string) (Record, error) {
	var rec Record
	if err := json.Unmarshal([]byte(encoded), &rec); err != nil {
		return Record{}, err
	}
	rec.Record.Body = denull(rec.Record.Body)
	rec.Record.Vars = denull(rec.Record.Vars)
	rec.Record.Attrs = denull(rec.Record.Attrs)
	return rec, nil
}

// jsonNull is what a nil json.RawMessage becomes on the way out.
const jsonNull = "null"

func denull(raw json.RawMessage) json.RawMessage {
	if string(raw) == jsonNull {
		return nil
	}
	return raw
}

// encodable makes a record safe to marshal.
//
// A json.RawMessage that is empty but not nil is not valid JSON, and marshalling
// one fails the whole record — "unexpected end of JSON input" — rather than the
// field. Nil and empty already mean the same thing to the store on the way out
// (it writes SQL NULL for both), so they are made the same here, on the way in.
//
// The caller survives this either way: a folder error stores the record as it
// stands. But it would do so with a warning per record, and folding would stop
// working for the trace, which is a lot to lose to a field nobody captured.
func encodable(r Record) Record {
	r.Record.Body = nilIfEmpty(r.Record.Body)
	r.Record.Vars = nilIfEmpty(r.Record.Vars)
	r.Record.Attrs = nilIfEmpty(r.Record.Attrs)
	return r
}

func nilIfEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// decodeOpen rebuilds a run from what Redis held.
func decodeOpen(fields, chunks map[string]string) (*Open, error) {
	first, err := decodeRecord(fields[fFirst])
	if err != nil {
		return nil, fmt.Errorf("fold: decode first record: %w", err)
	}

	open := &Open{
		Key:       KeyOf(first),
		First:     first,
		Count:     atoi(fields[fCount]),
		LastSeq:   int64(atoi(fields[fLastSeq])),
		LastEnd:   time.UnixMilli(int64(atoi(fields[fLastEnd]))).UTC(),
		Shape:     fields[fShape],
		Bytes:     atoi(fields[fBytes]),
		Truncated: fields[fTruncated] != "",
		Mergeable: fields[fMergeable] != "",
		Err:       fields[fErr],
		Dropped:   fields[fDropped] != "",
	}

	open.Chunks = make(map[int64]string, len(chunks))
	for seq, text := range chunks {
		open.Chunks[int64(atoi(seq))] = text
	}
	return open, nil
}

// toMap flattens the field/value array Redis returns for HGETALL.
func toMap(v any) map[string]string {
	flat, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		key, kok := flat[i].(string)
		val, vok := flat[i+1].(string)
		if kok && vok {
			out[key] = val
		}
	}
	return out
}

// boolArg encodes a flag the way the scripts read it: any non-empty string is
// true, so the false case is genuinely absent rather than the string "false".
func boolArg(b bool) string {
	if b {
		return "1"
	}
	return ""
}

// atoi parses a decimal Redis wrote, treating anything unreadable as zero. A
// corrupt field costs the fold its accuracy, never the batch its progress.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
