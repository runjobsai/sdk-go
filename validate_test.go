package runjobs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// helper: build a Schema literal in a one-liner.
func newSchema(inputs map[string]*Field, constraints ...Constraint) *Schema {
	return &Schema{Inputs: inputs, Constraints: constraints}
}

// ─── Per-field validation ────────────────────────────────────────────

func TestValidate_RequiredMissing(t *testing.T) {
	s := newSchema(map[string]*Field{
		"prompt": {Type: "string", Presence: PresenceRequired},
	})
	errs := s.ValidateRequest(map[string]any{})
	if len(errs) != 1 {
		t.Fatalf("want 1 err, got %d: %v", len(errs), errs)
	}
	if errs[0].Field != "prompt" || errs[0].Reason != "required" {
		t.Errorf("unexpected error: %+v", errs[0])
	}
}

func TestValidate_RequiredPresent(t *testing.T) {
	s := newSchema(map[string]*Field{
		"prompt": {Type: "string", Presence: PresenceRequired},
	})
	errs := s.ValidateRequest(map[string]any{"prompt": "hello"})
	if len(errs) != 0 {
		t.Errorf("want no errors, got %v", errs)
	}
}

func TestValidate_RequiredEmptyString(t *testing.T) {
	s := newSchema(map[string]*Field{
		"prompt": {Type: "string", Presence: PresenceRequired},
	})
	errs := s.ValidateRequest(map[string]any{"prompt": ""})
	if !errs.HasField("prompt") {
		t.Errorf("empty string for required field should error: %v", errs)
	}
}

func TestValidate_ForbiddenSet(t *testing.T) {
	s := newSchema(map[string]*Field{
		"prompt": {Type: "string", Presence: PresenceForbidden},
	})
	errs := s.ValidateRequest(map[string]any{"prompt": "should not be here"})
	if !errs.HasField("prompt") {
		t.Errorf("forbidden field set should error: %v", errs)
	}
}

func TestValidate_ForbiddenAbsent(t *testing.T) {
	s := newSchema(map[string]*Field{
		"prompt": {Type: "string", Presence: PresenceForbidden},
	})
	errs := s.ValidateRequest(map[string]any{}) // not set → fine
	if len(errs) != 0 {
		t.Errorf("forbidden absent should be OK, got %v", errs)
	}
}

func TestValidate_TypeMismatch(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		val  any
		want bool // expect a type error
	}{
		{"string-OK", "string", "x", false},
		{"string-bad", "string", 123.0, true},
		{"int-OK-float64", "int", 5.0, false}, // JSON decoding makes ints float64
		{"int-OK-int", "int", 5, false},
		{"int-bad", "int", "5", true},
		{"bool-OK", "bool", true, false},
		{"bool-bad", "bool", "true", true},
		{"url-OK-https", "url", "https://x.com/a", false},
		{"url-OK-data", "url", "data:image/png;base64,abc", false},
		{"url-bad-not-url", "url", "not a url", true},
		{"url-bad-not-string", "url", 5, true},
		{"url[]-OK", "url[]", []any{"https://a", "https://b"}, false},
		{"url[]-bad-mixed", "url[]", []any{"https://a", 5}, true},
		{"file-always-OK", "file", "anything", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSchema(map[string]*Field{
				"x": {Type: tc.typ, Presence: PresenceOptional},
			})
			errs := s.ValidateRequest(map[string]any{"x": tc.val})
			got := errs.HasField("x")
			if got != tc.want {
				t.Errorf("want err=%v, got %v (errs=%v)", tc.want, got, errs)
			}
		})
	}
}

func TestValidate_NumericBounds(t *testing.T) {
	min, max := 2.0, 20.0
	s := newSchema(map[string]*Field{
		"duration": {Type: "int", Min: &min, Max: &max},
	})
	cases := []struct {
		val     any
		wantErr bool
	}{
		{1.0, true}, // below min
		{2.0, false},
		{10.0, false},
		{20.0, false},
		{21.0, true}, // above max
	}
	for _, tc := range cases {
		errs := s.ValidateRequest(map[string]any{"duration": tc.val})
		if errs.HasField("duration") != tc.wantErr {
			t.Errorf("duration=%v want err=%v, got %v", tc.val, tc.wantErr, errs)
		}
	}
}

func TestValidate_Enum(t *testing.T) {
	s := newSchema(map[string]*Field{
		"aspect_ratio": {Type: "string", Enum: []any{"auto", "16:9", "9:16"}},
	})
	if errs := s.ValidateRequest(map[string]any{"aspect_ratio": "16:9"}); len(errs) != 0 {
		t.Errorf("16:9 should pass, got %v", errs)
	}
	if errs := s.ValidateRequest(map[string]any{"aspect_ratio": "21:9"}); !errs.HasField("aspect_ratio") {
		t.Errorf("21:9 should fail: %v", errs)
	}
}

func TestValidate_MaxItems(t *testing.T) {
	max := 2
	s := newSchema(map[string]*Field{
		"reference_image_urls": {Type: "url[]", MaxItems: &max},
	})
	if errs := s.ValidateRequest(map[string]any{
		"reference_image_urls": []any{"https://a", "https://b"},
	}); len(errs) != 0 {
		t.Errorf("at-max should pass: %v", errs)
	}
	if errs := s.ValidateRequest(map[string]any{
		"reference_image_urls": []any{"https://a", "https://b", "https://c"},
	}); !errs.HasField("reference_image_urls") {
		t.Errorf("over-max should fail")
	}
}

// ─── Cross-field constraints ─────────────────────────────────────────

func TestValidate_AnyOfRequired_NoneSet(t *testing.T) {
	s := newSchema(map[string]*Field{
		"prompt":           {Type: "string", Presence: PresenceOptional},
		"source_image_url": {Type: "url", Presence: PresenceOptional},
	}, Constraint{Kind: AnyOfRequired, Fields: []string{"prompt", "source_image_url"}})
	errs := s.ValidateRequest(map[string]any{})
	if len(errs) == 0 {
		t.Error("any_of_required with neither set should fail")
	}
}

func TestValidate_AnyOfRequired_OneSet(t *testing.T) {
	s := newSchema(map[string]*Field{
		"prompt":           {Type: "string", Presence: PresenceOptional},
		"source_image_url": {Type: "url", Presence: PresenceOptional},
	}, Constraint{Kind: AnyOfRequired, Fields: []string{"prompt", "source_image_url"}})
	errs := s.ValidateRequest(map[string]any{"prompt": "hi"})
	if len(errs) != 0 {
		t.Errorf("any_of_required with one set should pass: %v", errs)
	}
}

func TestValidate_MutuallyExclusive(t *testing.T) {
	s := newSchema(map[string]*Field{
		"voice":               {Type: "string", Presence: PresenceOptional},
		"reference_audio_url": {Type: "url", Presence: PresenceOptional},
	}, Constraint{Kind: MutuallyExclusive, Fields: []string{"voice", "reference_audio_url"}})

	// One set → OK.
	if errs := s.ValidateRequest(map[string]any{"voice": "alloy"}); len(errs) != 0 {
		t.Errorf("one-of mutex should pass: %v", errs)
	}
	// Both set → fail.
	errs := s.ValidateRequest(map[string]any{
		"voice":               "alloy",
		"reference_audio_url": "https://x/audio.wav",
	})
	if len(errs) == 0 {
		t.Error("both-of mutex should fail")
	}
}

func TestValidate_RequiresAll(t *testing.T) {
	s := newSchema(map[string]*Field{
		"audio":            {Type: "bool", Presence: PresenceOptional},
		"source_image_url": {Type: "url", Presence: PresenceOptional},
	}, Constraint{Kind: RequiresAll, When: "audio", Then: []string{"source_image_url"}})

	// when=false → constraint inactive.
	if errs := s.ValidateRequest(map[string]any{}); len(errs) != 0 {
		t.Errorf("inactive constraint should pass: %v", errs)
	}
	// when=true, then=missing → error. (audio:true is "set" by isEmpty's
	// non-nil semantics for non-empty bools.)
	errs := s.ValidateRequest(map[string]any{"audio": true})
	if len(errs) == 0 {
		t.Error("active constraint without then-fields should fail")
	}
	// when=true, then=set → OK.
	if errs := s.ValidateRequest(map[string]any{
		"audio":            true,
		"source_image_url": "https://x/a.png",
	}); len(errs) != 0 {
		t.Errorf("satisfied constraint should pass: %v", errs)
	}
}

func TestValidate_GroupMutex(t *testing.T) {
	// Seedance/Veo-style: keyframe block vs reference block. Inside
	// a block the fields can co-exist; across blocks they cannot.
	s := newSchema(map[string]*Field{
		"first_frame_url":      {Type: "url", Presence: PresenceOptional},
		"last_frame_url":       {Type: "url", Presence: PresenceOptional},
		"reference_image_urls": {Type: "url[]", Presence: PresenceOptional},
		"reference_video_urls": {Type: "url[]", Presence: PresenceOptional},
	}, Constraint{
		Kind: GroupMutex,
		Groups: [][]string{
			{"first_frame_url", "last_frame_url"},
			{"reference_image_urls", "reference_video_urls"},
		},
	})

	// Empty request → fine.
	if errs := s.ValidateRequest(map[string]any{}); len(errs) != 0 {
		t.Errorf("empty req should pass: %v", errs)
	}
	// Both fields in keyframe block → still fine (within-group co-exist).
	if errs := s.ValidateRequest(map[string]any{
		"first_frame_url": "https://x/a.png",
		"last_frame_url":  "https://x/b.png",
	}); len(errs) != 0 {
		t.Errorf("within-group co-existence should pass: %v", errs)
	}
	// Both fields in reference block → still fine.
	if errs := s.ValidateRequest(map[string]any{
		"reference_image_urls": []any{"https://x/r1.png"},
		"reference_video_urls": []any{"https://x/r1.mp4"},
	}); len(errs) != 0 {
		t.Errorf("within-group co-existence should pass: %v", errs)
	}
	// Across blocks → fail.
	errs := s.ValidateRequest(map[string]any{
		"first_frame_url":      "https://x/a.png",
		"reference_image_urls": []any{"https://x/r1.png"},
	})
	if len(errs) == 0 {
		t.Error("across-block use should fail")
	}
}

func TestValidate_PixelBounds(t *testing.T) {
	min, max := int64(1048576), int64(16777216) // 1MP – 16MP
	s := newSchema(map[string]*Field{
		"size": {Type: "string", Presence: PresenceOptional},
	}, Constraint{Kind: PixelBounds, Fields: []string{"size"}, Min: &min, Max: &max})

	if errs := s.ValidateRequest(map[string]any{"size": "1024x1024"}); len(errs) != 0 {
		t.Errorf("1MP should pass: %v", errs)
	}
	if errs := s.ValidateRequest(map[string]any{"size": "100x100"}); len(errs) == 0 {
		t.Error("10000 pixels should fail (below min)")
	}
	if errs := s.ValidateRequest(map[string]any{"size": "8192x8192"}); len(errs) == 0 {
		t.Error("67MP should fail (above max)")
	}
	// Bad format → reported as parse error.
	if errs := s.ValidateRequest(map[string]any{"size": "not-a-size"}); !errs.HasField("size") {
		t.Errorf("malformed size should fail: %v", errs)
	}
}

// ─── Integration: real LTX 2.3 schema ─────────────────────────────────

func TestValidate_LTX23_AudioToVideo(t *testing.T) {
	s := loadGoldenFixture(t, "ltx_2_3_audio_to_video.json")

	// Empty request — both audio and prompt-or-image missing.
	errs := s.ValidateRequest(map[string]any{})
	if !errs.HasField("source_audio_url") {
		t.Errorf("missing audio should error: %v", errs)
	}
	// No constraint-error field name match because we use slash-joined.
	hasAnyOfErr := false
	for _, e := range errs {
		if e.Reason != "" && (e.Field == "prompt/source_image_url" || e.Field == "source_image_url/prompt") {
			hasAnyOfErr = true
		}
	}
	if !hasAnyOfErr {
		t.Errorf("missing any_of should error; got %v", errs)
	}

	// Audio + prompt → OK.
	errs = s.ValidateRequest(map[string]any{
		"source_audio_url": "https://x/audio.mp3",
		"prompt":           "a cat singing",
	})
	if len(errs) != 0 {
		t.Errorf("audio+prompt should pass: %v", errs)
	}

	// Audio + image → OK.
	errs = s.ValidateRequest(map[string]any{
		"source_audio_url": "https://x/audio.mp3",
		"source_image_url": "https://x/cat.png",
	})
	if len(errs) != 0 {
		t.Errorf("audio+image should pass: %v", errs)
	}

	// Audio + image + invalid aspect → enum error.
	errs = s.ValidateRequest(map[string]any{
		"source_audio_url": "https://x/audio.mp3",
		"source_image_url": "https://x/cat.png",
		"aspect_ratio":     "21:9", // not in {auto,16:9,9:16}
	})
	if !errs.HasField("aspect_ratio") {
		t.Errorf("invalid aspect should fail: %v", errs)
	}

	// Audio + prompt + duration out of range.
	errs = s.ValidateRequest(map[string]any{
		"source_audio_url": "https://x/audio.mp3",
		"prompt":           "x",
		"duration":         1.0,
	})
	if !errs.HasField("duration") {
		t.Errorf("duration=1 should fail (min=2): %v", errs)
	}
}

// Wan animate-move: prompt forbidden, image+video required.
func TestValidate_WanAnimateMove(t *testing.T) {
	s := loadGoldenFixture(t, "wan_animate_move.json")

	// prompt forbidden — setting it errors.
	errs := s.ValidateRequest(map[string]any{
		"prompt":           "x",
		"source_image_url": "https://x/a.png",
		"source_video_url": "https://x/v.mp4",
	})
	if !errs.HasField("prompt") {
		t.Errorf("forbidden prompt should error: %v", errs)
	}
}

// CosyVoice: voice and reference_audio_url are mutually exclusive.
func TestValidate_CosyVoice_MutexVoiceClone(t *testing.T) {
	s := loadGoldenFixture(t, "cosyvoice.json")

	errs := s.ValidateRequest(map[string]any{
		"input":               "hello",
		"voice":               "亲切男声",
		"reference_audio_url": "https://x/clone.wav",
	})
	hasMutexErr := false
	for _, e := range errs {
		if e.Reason != "" && (e.Field == "voice/reference_audio_url" || e.Field == "reference_audio_url/voice") {
			hasMutexErr = true
		}
	}
	if !hasMutexErr {
		t.Errorf("mutex error expected, got %v", errs)
	}
}

// ─── Schema parsing ──────────────────────────────────────────────────

func TestModel_OptionsSchema(t *testing.T) {
	m := Model{
		Options: map[string]any{
			"version": float64(1),
			"inputs": map[string]any{
				"prompt": map[string]any{
					"type":     "string",
					"presence": "required",
				},
			},
		},
	}
	s, err := m.OptionsSchema()
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("schema should not be nil")
	}
	f, ok := s.Inputs["prompt"]
	if !ok {
		t.Fatal("prompt missing from inputs")
	}
	if f.Type != "string" || f.Presence != PresenceRequired {
		t.Errorf("unexpected prompt field: %+v", f)
	}
}

func TestModel_AcceptsField(t *testing.T) {
	m := Model{
		Options: map[string]any{
			"version": float64(1),
			"inputs": map[string]any{
				"audio":  map[string]any{"type": "url", "presence": "required"},
				"prompt": map[string]any{"type": "string", "presence": "forbidden"},
			},
		},
	}
	if !m.AcceptsField("audio") {
		t.Error("AcceptsField(audio) should be true")
	}
	if m.AcceptsField("prompt") {
		t.Error("AcceptsField(prompt) should be false (forbidden)")
	}
	if m.AcceptsField("nope") {
		t.Error("AcceptsField for absent field should be false")
	}
}

func TestModel_RequiresField(t *testing.T) {
	m := Model{
		Options: map[string]any{
			"version": float64(1),
			"inputs": map[string]any{
				"audio": map[string]any{"type": "url", "presence": "required"},
			},
		},
	}
	if !m.RequiresField("audio") {
		t.Error("audio should be required")
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────

// loadGoldenFixture reads a fixture from the gateway's testdata
// directory. Tests rely on file-path coupling so the SDK and gateway
// stay in sync without code-generation.
//
// Path: ../ai-gateway/internal/api/options_schema/testdata/<name>.
// If the file isn't present (e.g. running sdk-go tests without the
// gateway repo checked out next door), the test is skipped — not
// failed — so contributors who only cloned sdk-go can still
// `go test ./...` cleanly.
func loadGoldenFixture(t *testing.T, name string) *Schema {
	t.Helper()
	candidates := []string{
		filepath.Join("..", "ai-gateway", "internal", "api", "options_schema", "testdata", name),
		filepath.Join("testdata", name), // fallback if fixtures get vendored
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s Schema
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		return &s
	}
	t.Skipf("golden fixture %s not found in any known location; skipping", name)
	return nil
}
