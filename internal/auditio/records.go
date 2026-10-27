package auditio

import (
	"strings"
	"unicode/utf8"
)

// Records bounds UI/parser records independently of the full captured text.
// Oversized records disable detailed telemetry, not native capture or copying.
// Feed reports only the first limit event; callers must revoke preview approval.
func EmitChunks(text string, emit func(string) bool) {
	for len(text) > 0 {
		n := len(text)
		if n > ChunkBytes {
			n = ChunkBytes
			// Valid UTF-8 needs at most three continuation bytes. Invalid fallback
			// bytes must still make forward progress instead of finding no boundary.
			for n > ChunkBytes-3 && !utf8.RuneStart(text[n]) {
				n--
			}
		}
		if !emit(text[:n]) {
			return
		}
		text = text[n:]
	}
}

type Records struct {
	pending  string
	skipping bool
	Limited  bool
}

func (r *Records) Feed(text string, final bool, emit func(string)) bool {
	newlyLimited := false
	for len(text) > 0 {
		j := strings.IndexAny(text, "\r\n")
		n := j
		if n < 0 {
			n = len(text)
		}
		if !r.skipping {
			if len(r.pending)+n > ChunkBytes {
				if !r.Limited {
					newlyLimited = true
				}
				r.Limited = true
				r.pending = ""
				r.skipping = true
			} else {
				r.pending += text[:n]
			}
		}
		if j < 0 {
			break
		}
		if !r.skipping {
			emit(r.pending)
		}
		r.pending = ""
		r.skipping = false
		text = text[j+1:]
	}
	if final {
		if !r.skipping && r.pending != "" {
			emit(r.pending)
		}
		r.pending = ""
		r.skipping = false
	}
	return newlyLimited
}
