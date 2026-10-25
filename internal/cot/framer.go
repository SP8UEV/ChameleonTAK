// Package cot parses and builds Cursor-on-Target XML events.
package cot

import (
	"bufio"
	"encoding/xml"
	"errors"
	"io"
)

// MaxEventSize bounds memory used by a single client while an element is incomplete.
const MaxEventSize = 4 << 20

var ErrTooLarge = errors.New("zdarzenie CoT jest za duże")

// recorder hands bytes to the XML decoder one at a time and keeps a copy of everything
// read since the last complete element, so the original bytes can be forwarded untouched.
type recorder struct {
	r   *bufio.Reader
	buf []byte
}

func (r *recorder) ReadByte() (byte, error) {
	b, err := r.r.ReadByte()
	if err == nil {
		if len(r.buf) >= MaxEventSize {
			return 0, ErrTooLarge
		}
		r.buf = append(r.buf, b)
	}
	return b, err
}

func (r *recorder) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	b, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	p[0] = b
	return 1, nil
}

// Framer splits a stream of concatenated XML documents (<event>…</event><event>…) into
// raw top-level elements. XML declarations and whitespace between events are skipped.
type Framer struct {
	rec  *recorder
	dec  *xml.Decoder
	base int64 // stream offset of rec.buf[0]
}

func NewFramer(r io.Reader) *Framer {
	rec := &recorder{r: bufio.NewReaderSize(r, 32<<10)}
	dec := xml.NewDecoder(rec)
	dec.Strict = false
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	return &Framer{rec: rec, dec: dec}
}

// Next returns the raw bytes of the next complete top-level element.
func (f *Framer) Next() ([]byte, error) {
	depth := 0
	start := int64(-1)
	for {
		off := f.dec.InputOffset()
		tok, err := f.dec.RawToken()
		if err != nil {
			return nil, err
		}
		switch tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				start = off
			}
			depth++
		case xml.EndElement:
			depth--
			if depth < 0 {
				depth = 0
				continue
			}
			if depth == 0 && start >= 0 {
				end := f.dec.InputOffset()
				raw := make([]byte, end-start)
				copy(raw, f.rec.buf[start-f.base:end-f.base])
				f.rec.buf = append(f.rec.buf[:0], f.rec.buf[end-f.base:]...)
				f.base = end
				return raw, nil
			}
		default:
			if depth == 0 {
				// Drop prolog/whitespace bytes between elements.
				cur := f.dec.InputOffset()
				f.rec.buf = append(f.rec.buf[:0], f.rec.buf[cur-f.base:]...)
				f.base = cur
			}
		}
	}
}
