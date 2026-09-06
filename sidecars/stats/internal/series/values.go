package series

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

// Values is a positional vector of readings that survives JSON.
//
// The reason it exists rather than a plain []float64: encoding/json refuses to
// marshal NaN and the infinities, returning "json: unsupported value: NaN" for
// the whole document. NaN is this package's own encoding for a gap — a series
// the dictionary knows but the scrape did not report — so a plain []float64
// makes the one value the format needs to carry the one value it cannot. The
// failure is silent in the worst way: store.push logs the encode error and
// drops the row, nothing in the sampler is fatal, and because indices are
// append-only a series that stops being reported puts a NaN in every subsequent
// sample. The tier stops being written for the rest of the pod's life and the
// only evidence is a log line.
//
// So a gap is written as JSON null, which is what a reader would expect anyway,
// and read back as NaN so the collapse rules in rollup keep seeing the value
// they are written against.
//
// The infinities go the same way. A metric reading of infinity is a broken
// metric rather than a measurement, and the alternative is the same silent
// write failure over a number that was never usable.
type Values []float64

// nullLiteral is what a non-finite reading is written as, and recognised as.
var nullLiteral = []byte("null")

// MarshalJSON writes the vector as a JSON array, with every non-finite reading
// as null.
//
// Hand-rolled rather than a []*float64 round trip because this runs once per
// sample per pod, forever, and the allocation of a pointer per series is real:
// at 95 series and a one-second interval that is 342,000 pointers an hour to
// express "this number is fine".
func (v Values) MarshalJSON() ([]byte, error) {
	if v == nil {
		return nullLiteral, nil
	}

	var buf bytes.Buffer
	// One byte per delimiter plus a conservative allowance per number, so the
	// common case does not grow the buffer.
	buf.Grow(len(v)*8 + 2)
	buf.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			buf.WriteByte(',')
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			buf.Write(nullLiteral)
			continue
		}
		buf.Write(appendFloat(buf.AvailableBuffer(), f))
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// appendFloat writes one finite float64 exactly as encoding/json would.
//
// Reproduced rather than delegated because the sizing this feature was signed
// off on — 534 bytes a sample, 2.19 MiB a pod — was measured against the
// encoder's own output. A shortest-round-trip format like 'g' looks equivalent
// and is not: it renders 1234567.125 as 1.234567125e+06, which is longer for
// exactly the mid-sized numbers a memory gauge produces. There is a test
// comparing this against encoding/json's output for that reason.
func appendFloat(dst []byte, f float64) []byte {
	// 'f' everywhere except the extremes, where it would spell out the zeroes.
	format := byte('f')
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	dst = strconv.AppendFloat(dst, f, format, -1, 64)

	// strconv writes a two-digit exponent, JSON convention is the shorter one:
	// 1e-09 becomes 1e-9.
	if format == 'e' {
		if n := len(dst); n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst
}

// UnmarshalJSON reads an array of numbers and nulls, turning every null back
// into NaN so a decoded vector is indistinguishable from the one encoded.
func (v *Values) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), nullLiteral) {
		*v = nil
		return nil
	}

	var raw []*float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	out := make(Values, len(raw))
	for i, f := range raw {
		if f == nil {
			out[i] = math.NaN()
			continue
		}
		out[i] = *f
	}
	*v = out
	return nil
}
