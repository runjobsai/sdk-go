package runjobs

import (
	"encoding/base64"
	"net/http"
)

// EncodeImageURL wraps raw image bytes as a `data:<mime>;base64,<payload>`
// URI suitable for any *_url field that accepts data: URIs (FirstFrameURL,
// LastFrameURL, ReferenceImageURLs, SourceImageURL, etc.). The gateway
// stashes the data: URI as a short-lived hosted blob before forwarding to
// upstream, so callers don't have to host their own bytes anywhere.
//
// MIME is sniffed via http.DetectContentType — empty / non-image bytes
// default to "application/octet-stream" which most upstreams accept.
//
// Use this when you have raw bytes (file read, screenshot, on-the-fly
// render). When you already have a hosted https:// URL, pass it
// verbatim — no wrapping needed.
//
// Replaces the deprecated *FrameB64 / ReferenceImagesB64 fields, which
// were removed in favour of routing every payload through *_url.
func EncodeImageURL(raw []byte) string {
	mime := http.DetectContentType(raw)
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}
