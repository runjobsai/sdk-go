package runjobs

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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

// DecodeMediaURL is the inverse of EncodeImageURL — it resolves the URL
// shape carried in image / audio responses (`ImageResult.URL`,
// `SpeechResponse.AudioURL` if you unmarshal manually, etc.) into raw
// bytes plus the declared mime type. Two transport modes handled:
//
//   - "data:<mime>;base64,<payload>"  → decode the inline payload
//   - "https://...", "http://..."     → HTTP GET + read body, mime
//                                       comes from the response's
//                                       Content-Type header
//
// 60s timeout for the HTTP path — the gateway's blob endpoint is
// localhost-fast, but signed-URL CDNs can be sluggish. Use a context
// with your own deadline to override.
//
// Pairs with EncodeImageURL for symmetric input/output handling so
// callers see the same conceptual URL shape on both sides.
func DecodeMediaURL(ctx context.Context, url string) (data []byte, mime string, err error) {
	if strings.HasPrefix(url, "data:") {
		rest := strings.TrimPrefix(url, "data:")
		semi := strings.Index(rest, ";")
		comma := strings.Index(rest, ",")
		if semi == -1 || comma == -1 || comma < semi {
			return nil, "", fmt.Errorf("malformed data URI")
		}
		b, err := base64.StdEncoding.DecodeString(rest[comma+1:])
		if err != nil {
			return nil, "", fmt.Errorf("decode data URI: %w", err)
		}
		return b, rest[:semi], nil
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, "", fmt.Errorf("unsupported url scheme: %q", url)
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("upstream %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return body, resp.Header.Get("Content-Type"), nil
}
