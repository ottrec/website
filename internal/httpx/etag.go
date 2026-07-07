package httpx

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash"
	"net/http"
	"os"
	"slices"
	"strings"
)

// ETag is a formatted entity tag, e.g. `"ABC123-gzip"` or `W/"ABC123"`.
type ETag string

// Weaken marks the etag as weak.
func (e ETag) Weaken() ETag {
	if strings.HasPrefix(string(e), "W/") {
		return e
	}
	return "W/" + e
}

// Handled sets the ETag header and, when the request's If-None-Match matches
// it, responds with 304 Not Modified, reporting whether the request was
// handled. Call it after negotiating the content encoding and setting
// Cache-Control, and before writing the body.
func (e ETag) Handled(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("ETag", string(e))
	// TODO: maybe actually parse it
	if slices.Contains(r.Header.Values("If-None-Match"), string(e)) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// MakeETag formats tag (usually a content hash) as a strong ETag, qualified by
// the content encoding (if any) so representations validate independently.
func MakeETag(tag, encoding string) ETag {
	if encoding != "" {
		tag += "-" + encoding
	}
	return ETag(`"` + tag + `"`)
}

// ETagBuilder accumulates inputs into a hashed [ETag].
type ETagBuilder struct {
	h        hash.Hash
	encoding string
}

// NewETag starts building a strong ETag from the mixed-in parts. Use
// [ETag.Weaken] on the result for a weak one.
func NewETag() *ETagBuilder {
	return &ETagBuilder{h: sha1.New()}
}

// Mix mixes in the parts.
func (b *ETagBuilder) Mix(parts ...string) *ETagBuilder {
	for _, p := range parts {
		b.h.Write([]byte{0})
		b.h.Write([]byte(p))
	}
	return b
}

// MixBytes mixes in raw bytes.
func (b *ETagBuilder) MixBytes(p ...byte) *ETagBuilder {
	b.h.Write(p)
	return b
}

// MixExe mixes in the hash of the current binary, plus anything added with
// [AddExeExtra].
func (b *ETagBuilder) MixExe() *ETagBuilder {
	return b.Mix(exehash)
}

// MixVary mixes in the request values of every header named by the response
// Vary header, so the etag is keyed by what the response varied on.
func (b *ETagBuilder) MixVary(respHeader, reqHeader http.Header) *ETagBuilder {
	for _, k := range respHeader.Values("Vary") {
		b.h.Write([]byte{0})
		b.h.Write([]byte(k))
		for _, v := range reqHeader.Values(k) {
			b.h.Write(binary.LittleEndian.AppendUint64(nil, uint64(len(v))))
			b.h.Write([]byte(v))
		}
	}
	return b
}

// Encoding qualifies the etag with the content encoding so representations
// validate independently.
func (b *ETagBuilder) Encoding(encoding string) *ETagBuilder {
	b.encoding = encoding
	return b
}

// ETag finalizes the tag.
func (b *ETagBuilder) ETag() ETag {
	return MakeETag(base32.StdEncoding.EncodeToString(b.h.Sum(nil)), b.encoding)
}

// AddExeExtra mixes content into the hash used by [ETagBuilder.MixExe], for
// startup-time configuration that affects rendered output. It must be called
// before anything is rendered.
func AddExeExtra(content string) {
	sum := sha1.Sum([]byte(content))
	exehash += base32.StdEncoding.EncodeToString(sum[:])
}

// exehash is a hash of the current binary for use in etags.
var exehash = func() string {
	exe, err := os.Executable()
	if err != nil {
		panic(fmt.Errorf("exehash: %w", err))
	}
	buf, err := os.ReadFile(exe)
	if err != nil {
		panic(fmt.Errorf("exehash: %w", err))
	}
	sum := sha1.Sum(buf)
	return base32.StdEncoding.EncodeToString(sum[:])
}()
