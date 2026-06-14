package types

// Variables is a named map of arbitrary per-message values. Its typed
// accessors apply a documented coercion policy so callers do not have to
// reason about whether a value was set directly in Go or decoded from JSON
// (where every number becomes float64).
type Variables map[string]any

// Set stores value under key, allocating the map if needed.
func (v *Variables) Set(key string, value any) {
	if *v == nil {
		*v = make(Variables)
	}
	(*v)[key] = value
}

// String returns the value at key as a string. ok is false if the key is
// absent or the value is not a string.
func (v Variables) String(key string) (string, bool) {
	s, ok := v[key].(string)
	return s, ok
}

// Bool returns the value at key as a bool. ok is false if the key is absent
// or the value is not a bool.
func (v Variables) Bool(key string) (bool, bool) {
	b, ok := v[key].(bool)
	return b, ok
}

// Int returns the value at key as an int. It accepts int, int64 and float64
// (the latter covers JSON-decoded numbers) provided the value has no
// fractional part. ok is false otherwise.
func (v Variables) Int(key string) (int, bool) {
	switch n := v[key].(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// Float returns the value at key as a float64. It accepts float64, int and
// int64. ok is false if the key is absent or not numeric.
func (v Variables) Float(key string) (float64, bool) {
	switch n := v[key].(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
