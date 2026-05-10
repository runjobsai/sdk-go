package runjobs

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidationError describes a single per-field validation failure.
// The Field name matches the canonical request field key
// (e.g. "source_audio_url"); Reason is a short human-readable
// explanation suitable for surfacing to the end user verbatim.
type ValidationError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// ValidationErrors is a flat list of validator failures. Multiple
// errors are returned at once so the UI can surface them all
// (rather than fix-one-resubmit cycles).
type ValidationErrors []ValidationError

// Error joins all underlying errors into one message — convenient
// for callers that want a single-line diagnostic.
func (errs ValidationErrors) Error() string {
	if len(errs) == 0 {
		return ""
	}
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

// HasField reports whether the error list mentions the named field.
// Convenient for "is there a problem with `prompt`?" style checks.
func (errs ValidationErrors) HasField(name string) bool {
	for _, e := range errs {
		if e.Field == name {
			return true
		}
	}
	return false
}

// ValidateRequest checks that `req` (a flattened name→value map of
// the request body) satisfies the schema's input constraints. Three
// passes:
//
//  1. Per-field type & enum & numeric-bound check
//  2. Per-field presence check (required / forbidden)
//  3. Cross-field constraint check
//
// Unknown fields in `req` (not declared in s.Inputs) are silently
// IGNORED — vendor-specific passthroughs (`shot_type`, `prompt_extend`)
// shouldn't fail validation. The gateway still validates them
// server-side if needed.
//
// Caller is responsible for flattening the request into the map.
// For Go SDK callers, convert via:
//
//	json.Marshal(typed) → unmarshal into map[string]any
//
// or hand-build the map. nil request = empty map.
func (s *Schema) ValidateRequest(req map[string]any) ValidationErrors {
	if s == nil {
		return nil // no schema → no validation
	}
	if req == nil {
		req = map[string]any{}
	}
	var errs ValidationErrors

	// Pass 1+2: per-field. Type/enum/bound checks first; then
	// presence, since a field with presence:"forbidden" should
	// error even if the value is malformed.
	for name, field := range s.Inputs {
		val, set := req[name]
		if set && !isEmpty(val) {
			errs = append(errs, validateFieldValue(name, field, val)...)
		}
		errs = append(errs, validatePresence(name, field, val, set)...)
	}

	// Pass 3: cross-field constraints.
	for _, c := range s.Constraints {
		errs = append(errs, validateConstraint(c, req)...)
	}

	return errs
}

// validateFieldValue checks the value of a SET field against its
// type, enum, and numeric bounds. Skips presence (handled separately)
// so a field with the wrong type but presence: "optional" still
// reports a type error.
func validateFieldValue(name string, f *Field, val any) ValidationErrors {
	var errs ValidationErrors

	// Type check.
	if !typeMatches(f.Type, val) {
		errs = append(errs, ValidationError{
			Field:  name,
			Reason: fmt.Sprintf("expected type %s, got %s", f.Type, describeType(val)),
		})
		// Bounds & enum checks below assume the type was right —
		// skip them when it wasn't.
		return errs
	}

	// Enum check.
	if len(f.Enum) > 0 && !inEnum(val, f.Enum) {
		errs = append(errs, ValidationError{
			Field:  name,
			Reason: fmt.Sprintf("must be one of %v", f.Enum),
		})
	}

	// Numeric bounds.
	if f.Type == "int" || f.Type == "float" {
		if n, ok := numberValue(val); ok {
			if f.Min != nil && n < *f.Min {
				errs = append(errs, ValidationError{
					Field: name, Reason: fmt.Sprintf("must be ≥ %v", *f.Min),
				})
			}
			if f.Max != nil && n > *f.Max {
				errs = append(errs, ValidationError{
					Field: name, Reason: fmt.Sprintf("must be ≤ %v", *f.Max),
				})
			}
		}
	}

	// Array max_items.
	if f.MaxItems != nil {
		if arr, ok := val.([]any); ok && len(arr) > *f.MaxItems {
			errs = append(errs, ValidationError{
				Field: name, Reason: fmt.Sprintf("at most %d items", *f.MaxItems),
			})
		}
	}

	return errs
}

// validatePresence checks that required fields are set and forbidden
// fields are not.
func validatePresence(name string, f *Field, val any, set bool) ValidationErrors {
	switch f.Presence {
	case PresenceRequired:
		if !set || isEmpty(val) {
			return ValidationErrors{{Field: name, Reason: "required"}}
		}
	case PresenceForbidden:
		if set && !isEmpty(val) {
			return ValidationErrors{{Field: name, Reason: "must not be set for this model"}}
		}
	}
	return nil
}

// validateConstraint dispatches by Kind. Unknown Kinds are skipped
// (forward-compat: the gateway may add new constraint kinds before
// SDKs upgrade).
func validateConstraint(c Constraint, req map[string]any) ValidationErrors {
	switch c.Kind {
	case AnyOfRequired:
		for _, name := range c.Fields {
			if v, ok := req[name]; ok && !isEmpty(v) {
				return nil
			}
		}
		return ValidationErrors{{
			Field:  strings.Join(c.Fields, "/"),
			Reason: fmt.Sprintf("at least one of %s is required", strings.Join(c.Fields, ", ")),
		}}

	case MutuallyExclusive:
		set := []string{}
		for _, name := range c.Fields {
			if v, ok := req[name]; ok && !isEmpty(v) {
				set = append(set, name)
			}
		}
		if len(set) > 1 {
			return ValidationErrors{{
				Field:  strings.Join(set, "/"),
				Reason: fmt.Sprintf("at most one of %s may be set (got: %s)", strings.Join(c.Fields, ", "), strings.Join(set, ", ")),
			}}
		}

	case GroupMutex:
		// Active group = any field in the group is set.
		var activeGroups [][]string
		var activeFields []string
		for _, g := range c.Groups {
			groupActive := false
			for _, name := range g {
				if v, ok := req[name]; ok && !isEmpty(v) {
					groupActive = true
					activeFields = append(activeFields, name)
				}
			}
			if groupActive {
				activeGroups = append(activeGroups, g)
			}
		}
		if len(activeGroups) > 1 {
			labels := make([]string, len(c.Groups))
			for i, g := range c.Groups {
				labels[i] = "[" + strings.Join(g, "+") + "]"
			}
			return ValidationErrors{{
				Field:  strings.Join(activeFields, "/"),
				Reason: fmt.Sprintf("at most one of %s may be used (got: %s)", strings.Join(labels, " or "), strings.Join(activeFields, ", ")),
			}}
		}

	case RequiresAll:
		whenVal, whenOK := req[c.When]
		if !whenOK || isEmpty(whenVal) {
			return nil // when-field unset → constraint inactive
		}
		var missing []string
		for _, name := range c.Then {
			if v, ok := req[name]; !ok || isEmpty(v) {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return ValidationErrors{{
				Field:  strings.Join(missing, "/"),
				Reason: fmt.Sprintf("required when %s is set", c.When),
			}}
		}

	case PixelBounds:
		// fields[0] is expected to be a "WxH" string (or "W*H" / "W×H");
		// product must fall in [Min, Max]. Skip silently if field is
		// missing — Field-level presence check is responsible for that.
		if len(c.Fields) == 0 {
			return nil
		}
		raw, ok := req[c.Fields[0]].(string)
		if !ok || raw == "" {
			return nil
		}
		w, h, ok := parsePixelDims(raw)
		if !ok {
			return ValidationErrors{{
				Field:  c.Fields[0],
				Reason: fmt.Sprintf("expected WxH dimensions, got %q", raw),
			}}
		}
		px := int64(w) * int64(h)
		var errs ValidationErrors
		if c.Min != nil && px < *c.Min {
			errs = append(errs, ValidationError{
				Field:  c.Fields[0],
				Reason: fmt.Sprintf("image too small: %dx%d = %d pixels, minimum %d", w, h, px, *c.Min),
			})
		}
		if c.Max != nil && px > *c.Max {
			errs = append(errs, ValidationError{
				Field:  c.Fields[0],
				Reason: fmt.Sprintf("image too large: %dx%d = %d pixels, maximum %d", w, h, px, *c.Max),
			})
		}
		return errs
	}
	return nil
}

// ─── Type helpers ────────────────────────────────────────────────────

// typeMatches reports whether val matches the declared schema type
// loosely enough for JSON-decoded input (ints arrive as float64 from
// json.Unmarshal). file is always accepted (informational only —
// can't validate multipart bytes from this layer).
func typeMatches(typ string, val any) bool {
	switch typ {
	case "string":
		_, ok := val.(string)
		return ok
	case "int", "float":
		_, ok := numberValue(val)
		return ok
	case "bool":
		_, ok := val.(bool)
		return ok
	case "url":
		s, ok := val.(string)
		return ok && isURLLike(s)
	case "url[]":
		arr, ok := val.([]any)
		if !ok {
			return false
		}
		for _, item := range arr {
			s, ok := item.(string)
			if !ok || !isURLLike(s) {
				return false
			}
		}
		return true
	case "string[]":
		arr, ok := val.([]any)
		if !ok {
			return false
		}
		for _, item := range arr {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	case "file":
		// Multipart upload — can't validate from a JSON-flattened
		// view. Accept any value (caller is responsible for the
		// actual upload).
		return true
	}
	// Unknown type → don't error (forward-compat).
	return true
}

func describeType(val any) string {
	switch val.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case float64, float32, int, int64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", val)
}

func numberValue(val any) (float64, bool) {
	switch n := val.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}

func isURLLike(s string) bool {
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "data:")
}

// isEmpty reports whether val should be treated as "not set" for
// presence checks. nil, "", empty array all qualify.
func isEmpty(val any) bool {
	switch v := val.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	}
	return false
}

func inEnum(val any, enum []any) bool {
	for _, e := range enum {
		if equalLoose(val, e) {
			return true
		}
	}
	return false
}

// equalLoose compares two JSON-decoded values. Numbers are compared
// as float64 to bridge int / float64 ambiguity from json.Unmarshal.
func equalLoose(a, b any) bool {
	if af, aok := numberValue(a); aok {
		if bf, bok := numberValue(b); bok {
			return af == bf
		}
		return false
	}
	return a == b
}

// parsePixelDims parses "WxH" / "W*H" / "W×H" strings. Returns
// 0,0,false on parse failure.
func parsePixelDims(s string) (int, int, bool) {
	for _, sep := range []string{"x", "X", "*", "×"} {
		if idx := strings.Index(s, sep); idx > 0 {
			ws := s[:idx]
			hs := s[idx+len(sep):]
			w, errw := strconv.Atoi(strings.TrimSpace(ws))
			h, errh := strconv.Atoi(strings.TrimSpace(hs))
			if errw == nil && errh == nil && w > 0 && h > 0 {
				return w, h, true
			}
		}
	}
	return 0, 0, false
}
