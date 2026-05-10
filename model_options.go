package runjobs

import "encoding/json"

// Schema mirrors the wire shape of `options` returned by /v1/models —
// see API.md "Options schema" for the full contract. SDK clients
// consume it as the single source of truth for what each model accepts.
type Schema struct {
	// Inputs maps canonical request field names (matching the JSON
	// tags on Image/Video/Speech/Embed/Transcribe params) to per-
	// field constraints. A field absent from the map is NOT accepted
	// — clients should hide its UI and the gateway will 400 if set.
	Inputs map[string]*Field `json:"inputs,omitempty"`

	// Constraints captures cross-field rules (any_of_required,
	// mutually_exclusive, ...). See API.md for the vocabulary.
	Constraints []Constraint `json:"constraints,omitempty"`

	// Catalog holds rich content (voices, emotions) that an enum
	// can't represent on its own.
	Catalog *Catalog `json:"catalog,omitempty"`
}

// Field describes one input parameter the model accepts. Mirror of
// internal/api/options_schema/types.go on the gateway side.
type Field struct {
	Type     string   `json:"type"`
	Presence Presence `json:"presence,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Default  any      `json:"default,omitempty"`
	Enum     []any    `json:"enum,omitempty"`
	MaxItems *int     `json:"max_items,omitempty"`
	Role     string   `json:"role,omitempty"`

	// Display metadata — drives auto-rendered playground / form UIs.
	// Optional; clients fall back to type/role-based rendering when
	// absent. See API.md "Options schema" for the full vocabulary.

	// Label is the human-readable display label (e.g. "Source Audio").
	// Falls back to the field name when empty.
	Label string `json:"label,omitempty"`

	// Help is a one-sentence explainer rendered as tooltip / sub-text.
	// Surfaces implicit constraints the structured schema can't
	// capture (e.g. "audio must be 2–20 seconds").
	Help string `json:"help,omitempty"`

	// Widget overrides the default widget the renderer would pick
	// from Type:
	//   "textarea"     — large multi-line string
	//   "slider"       — numeric with min/max
	//   "radio"        — small enum
	//   "voice_picker" — string with values from catalog.voices
	//   "code"         — long string with syntax highlighting
	// Empty → renderer uses the type-based default. Unknown widgets
	// also fall back to the default (forward-compat).
	Widget string `json:"widget,omitempty"`

	// UI is a nested object for presentational hints not on
	// dedicated fields. Reserved keys: "group" (string — sectional
	// grouping like "main"/"advanced"/"output"), "order" (int —
	// display order within the group; lower first).
	UI map[string]any `json:"ui,omitempty"`
}

// Presence enumerates allowed values for Field.Presence.
type Presence string

const (
	PresenceRequired  Presence = "required"
	PresenceOptional  Presence = "optional"
	PresenceForbidden Presence = "forbidden"
)

// ConstraintKind enumerates the cross-field rule types.
type ConstraintKind string

const (
	AnyOfRequired ConstraintKind = "any_of_required"
	// MutuallyExclusive: at most one of Fields may be set. For
	// block-level XOR (multiple fields per side) prefer GroupMutex.
	MutuallyExclusive ConstraintKind = "mutually_exclusive"
	// GroupMutex: at most one of Groups may have any field set;
	// fields inside the same group can co-exist freely. Used for
	// keyframe-block vs reference-block XOR on Seedance/Veo.
	GroupMutex  ConstraintKind = "group_mutex"
	RequiresAll ConstraintKind = "requires_all"
	PixelBounds ConstraintKind = "pixel_bounds"
)

// Constraint expresses a cross-field rule.
type Constraint struct {
	Kind   ConstraintKind `json:"kind"`
	Fields []string       `json:"fields,omitempty"`
	// Groups is used by GroupMutex: at most one group may have any
	// field set; fields inside the same group can co-exist.
	Groups [][]string `json:"groups,omitempty"`
	When   string     `json:"when,omitempty"`
	Then   []string   `json:"then,omitempty"`
	Min    *int64     `json:"min,omitempty"`
	Max    *int64     `json:"max,omitempty"`
}

// Catalog holds rich content that enum values can't capture.
type Catalog struct {
	Voices   []map[string]any `json:"voices,omitempty"`
	Emotions []string         `json:"emotions,omitempty"`
}

// OptionsSchema parses the model's wire-format `options` blob into
// the typed Schema. Returns nil + an error if the blob is malformed
// or its inner shape can't be parsed — caller can
// fall back to whatever default behaviour they had pre-schema.
//
// The Model.Options field stays as `map[string]any` for backwards
// compatibility with callers that read individual keys directly;
// OptionsSchema is the recommended path for new code.
func (m Model) OptionsSchema() (*Schema, error) {
	if len(m.Options) == 0 {
		return nil, nil
	}
	// Round-trip via JSON — Model.Options was decoded into
	// map[string]any (lossy for typed Field/Constraint structs),
	// so we re-encode and parse into the typed shape.
	raw, err := json.Marshal(m.Options)
	if err != nil {
		return nil, err
	}
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// AcceptsField returns true if the model's Schema declares an
// Inputs entry for `name` with presence != "forbidden". Convenience
// for "should I show this UI chip" checks. Falls back to false on
// schema parse errors.
func (m Model) AcceptsField(name string) bool {
	s, err := m.OptionsSchema()
	if err != nil || s == nil {
		return false
	}
	f, ok := s.Inputs[name]
	if !ok {
		return false
	}
	return f.Presence != PresenceForbidden
}

// RequiresField returns true if the model's Schema marks `name` as
// presence: "required". Use for "should I red-star this UI input"
// decisions and for guarding submit buttons.
func (m Model) RequiresField(name string) bool {
	s, err := m.OptionsSchema()
	if err != nil || s == nil {
		return false
	}
	f, ok := s.Inputs[name]
	if !ok {
		return false
	}
	return f.Presence == PresenceRequired
}

// AllowedValuesFor returns the discrete enum (if any) declared for
// the given field, or nil if the schema places no enum constraint
// on it. Use to populate dropdown options on the client.
func (m Model) AllowedValuesFor(name string) []any {
	s, err := m.OptionsSchema()
	if err != nil || s == nil {
		return nil
	}
	f, ok := s.Inputs[name]
	if !ok {
		return nil
	}
	return f.Enum
}
