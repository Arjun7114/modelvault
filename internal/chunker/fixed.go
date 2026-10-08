package chunker

import (
	"errors"
	"io"
)

// FixedChunker splits input into fixed-size chunks of Size bytes. The final
// chunk may be smaller. It's the simplest possible boundary strategy and the
// baseline we'll later compare content-defined chunking against in Phase 2.
type FixedChunker struct {
	Size int
}

// NewFixed returns a FixedChunker with the given chunk size in bytes.
func NewFixed(size int) *FixedChunker {
	return &FixedChunker{Size: size}
}

// Split reads r to EOF, calling fn once per fixed-size chunk, in order.
func (c *FixedChunker) Split(r io.Reader, fn func(chunk []byte) error) error {
	if c.Size <= 0 {
		return errors.New("chunker: size must be positive")
	}

	buf := make([]byte, c.Size)
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			// buf is reused every iteration, so hand fn its OWN copy.
			// This matters in Phase 3, where chunks travel across goroutines.
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if ferr := fn(chunk); ferr != nil {
				return ferr
			}
		}
		switch err {
		case nil:
			continue // full chunk read; keep going
		case io.EOF, io.ErrUnexpectedEOF:
			return nil // clean end, or a short final chunk: done
		default:
			return err // a real read error
		}
	}
}