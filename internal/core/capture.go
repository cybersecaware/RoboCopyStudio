package core

import (
	"errors"
	"strings"
)

// MaxLogRecordBytes bounds an ANSI record's bytes, excluding CR/LF delimiters.
// UTF-16 decoding remains streaming and is not a record-oriented protocol.
const MaxLogRecordBytes = 1 << 20

// LogTarget describes the last explicit native log switch. Earlier switches
// remain untouched: Robocopy can create/truncate those paths before using this one.
type LogTarget struct {
	Path            string
	Unicode, Append bool
}

func SelectedLog(args []string) LogTarget {
	var log LogTarget
	for _, arg := range args {
		n := SwitchName(arg)
		switch n {
		case "LOG", "LOG+", "UNILOG", "UNILOG+":
			if i := strings.IndexByte(arg, ':'); i >= 0 {
				log = LogTarget{Path: arg[i+1:], Unicode: strings.HasPrefix(n, "UNI"), Append: strings.HasSuffix(n, "+")}
			}
		}
	}
	return log
}

// LogOffset skips existing append history unless an earlier native switch
// overwrites that same destination first. samePath resolves OS-specific aliases.
func LogOffset(args []string, initial int64, samePath func(string, string) bool) int64 {
	selected := SelectedLog(args)
	if !selected.Append {
		return 0
	}
	for _, arg := range args {
		log := SelectedLog([]string{arg})
		if log.Path != "" && !log.Append && samePath(log.Path, selected.Path) {
			return 0
		}
	}
	return initial
}

// LogDecoder preserves partial UTF-16 units/surrogate pairs and ANSI records
// across file-tail reads. ANSI decoding is supplied by the Windows code page API.
type LogDecoder struct {
	Unicode bool
	ANSI    func([]byte) string
	// Err is sticky. After a record exceeds the ANSI limit, no more text is
	// decoded (including final flush); callers must mark capture incomplete.
	Err   error
	carry []byte
}

func (d *LogDecoder) Feed(b []byte, final bool) string {
	if d.Err != nil {
		return ""
	}
	if !d.Unicode {
		return d.feedANSI(b, final)
	}
	d.carry = append(d.carry, b...)
	n := len(d.carry) / 2 * 2
	if !final && n >= 2 {
		v := uint16(d.carry[n-2]) | uint16(d.carry[n-1])<<8
		if v >= 0xd800 && v <= 0xdbff {
			n -= 2
		}
	}
	text := DecodeLog(d.carry[:n])
	d.carry = append(d.carry[:0], d.carry[n:]...)
	if final {
		d.carry = nil
	}
	return text
}

func (d *LogDecoder) decodeANSI(b []byte) string {
	if d.ANSI != nil {
		return d.ANSI(b)
	}
	return string(b)
}

func (d *LogDecoder) feedANSI(b []byte, final bool) string {
	var out strings.Builder
	emitted := false
	for len(b) > 0 {
		i := 0
		for i < len(b) && b[i] != '\r' && b[i] != '\n' {
			i++
		}
		// Check before copying, including when one tail read is itself oversized.
		if i > MaxLogRecordBytes-len(d.carry) {
			d.Err = errors.New("ANSI log record exceeds 1 MiB limit; capture incomplete")
			d.carry = nil
			return out.String()
		}
		n := i
		if i < len(b) {
			n++
		}
		// Keep capacity bounded too; append's geometric growth can overshoot.
		needed := len(d.carry) + n
		if needed > cap(d.carry) {
			capacity := cap(d.carry) * 2
			if capacity < needed {
				capacity = needed
			}
			if capacity > MaxLogRecordBytes+1 {
				capacity = MaxLogRecordBytes + 1
			}
			next := make([]byte, len(d.carry), capacity)
			copy(next, d.carry)
			d.carry = next
		}
		d.carry = append(d.carry, b[:n]...)
		b = b[n:]
		if n > i {
			out.WriteString(d.decodeANSI(d.carry))
			d.carry = d.carry[:0]
			emitted = true
		}
	}
	if final {
		out.WriteString(d.decodeANSI(d.carry))
		d.carry = nil
	} else if !emitted {
		// Preserve the empty callback on reads with no complete record.
		out.WriteString(d.decodeANSI(nil))
	}
	return out.String()
}
