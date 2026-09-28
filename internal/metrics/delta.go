package metrics

import (
	"encoding/json"
	"strconv"
)

// MetricDeltas holds the per-field difference head minus base for every
// RawMetrics field, and encodes as a JSON object with the same names in the
// same order. Bool fields are reported as -1 (true to false), 0 (unchanged)
// or +1 (false to true) and encode, like counts, as integers. A v1 field is
// null when head did not compute it; when head computed it and base did
// not, the base is treated as zero so a new package against a zero baseline
// reports the head value.
type MetricDeltas struct {
	d [numFields]delta
}

// delta is one field's difference.
type delta struct {
	// v is head minus base.
	v float64
	// ok is false when head did not compute the field.
	ok bool
	// integral says the field is a count or a bool, encoded as an integer.
	integral bool
}

// Delta returns head minus base for every field, where the receiver is head.
// Pass a zero RawMetrics as base for a package that is new at head.
func (m *RawMetrics) Delta(base RawMetrics) MetricDeltas {
	var d MetricDeltas
	hs, bs := m.slots(), base.slots()
	for i := range hs {
		h, ok, k := read(hs[i])
		if !ok {
			continue
		}
		b, _, _ := read(bs[i])
		d.d[i] = delta{v: h - b, ok: true, integral: k != kindFloat}
	}
	return d
}

// Value returns the delta named by its JSON field name as a float64. The
// second result is false when the name is unknown or names a v1 field whose
// delta is null.
func (d *MetricDeltas) Value(name string) (float64, bool) {
	i := fieldIndex(name)
	if i < 0 || !d.d[i].ok {
		return 0, false
	}
	return d.d[i].v, true
}

// MarshalJSON encodes d as an object keyed by the RawMetrics JSON names, in
// MetricNames order: null for a field head did not compute, an integer for
// a count or a bool, and a number as encoding/json writes a float64
// otherwise.
func (d MetricDeltas) MarshalJSON() ([]byte, error) {
	b := []byte{'{'}
	for i, name := range MetricNames() {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendQuote(b, name)
		b = append(b, ':')
		v, err := d.d[i].encode()
		if err != nil {
			return nil, err
		}
		b = append(b, v...)
	}
	return append(b, '}'), nil
}

// encode returns x in JSON as MetricDeltas.MarshalJSON describes.
func (x delta) encode() ([]byte, error) {
	switch {
	case !x.ok:
		return []byte("null"), nil
	case x.integral:
		return strconv.AppendInt(nil, int64(x.v), 10), nil
	default:
		return json.Marshal(x.v)
	}
}
