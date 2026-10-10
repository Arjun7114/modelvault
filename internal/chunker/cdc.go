package chunker

import (
	"bufio"
	"errors"
	"io"
)

// gearTable maps each byte value to a pseudo-random 64-bit number. It is the
// heart of the Gear rolling hash: as each byte is mixed in, the hash rolls
// forward and older bytes decay out of the top as the value is shifted left.
//
// The table is derived deterministically (via splitmix64) so every build, on
// every machine, produces identical chunk boundaries. That matters: a chunk's
// hash is its address, so dedup only works if boundaries are perfectly stable.
var gearTable = buildGearTable()

func buildGearTable() [256]uint64 {
	var t [256]uint64
	for i := 0; i < 256; i++ {
		t[i] = splitmix64(uint64(i))
	}
	return t
}

// splitmix64 is a small, well-known deterministic integer hash. It's used only
// to fill the gear table — not for any security purpose.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	z := x
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// CDCChunker splits input at content-defined boundaries using a Gear rolling
// hash (the scheme behind FastCDC). Because a boundary is chosen by the content
// around it, inserting or deleting bytes shifts only the chunks near the edit —
// the rest keep their boundaries, their hashes, and therefore de-duplicate
// against earlier backups. This is the key advantage over FixedChunker.
type CDCChunker struct {
	min  int
	max  int
	mask uint64
}

var _ Chunker = (*CDCChunker)(nil)

// NewCDC returns a content-defined chunker. Each chunk is at least minSize and
// at most maxSize bytes. avgBits sets the target average chunk size to 2^avgBits
// bytes (e.g. avgBits=13 targets ~8 KiB): a boundary is cut once the chunk is at
// least minSize and the low avgBits bits of the rolling hash are zero, or
// unconditionally once the chunk reaches maxSize.
//
// Expected chunk size is roughly minSize + 2^avgBits.
func NewCDC(minSize, avgBits, maxSize int) (*CDCChunker, error) {
	if minSize <= 0 || maxSize <= 0 {
		return nil, errors.New("chunker: sizes must be positive")
	}
	if minSize > maxSize {
		return nil, errors.New("chunker: minSize must not exceed maxSize")
	}
	if avgBits < 1 || avgBits > 32 {
		return nil, errors.New("chunker: avgBits out of range (1..32)")
	}
	return &CDCChunker{
		min:  minSize,
		max:  maxSize,
		mask: (uint64(1) << uint(avgBits)) - 1,
	}, nil
}

// Split reads r to EOF, calling fn once per content-defined chunk, in order.
func (c *CDCChunker) Split(r io.Reader, fn func(chunk []byte) error) error {
	br := bufio.NewReader(r)
	cur := make([]byte, 0, c.max)
	var h uint64

	// flush emits the current chunk (if any) and resets state for the next one.
	flush := func() error {
		if len(cur) == 0 {
			return nil
		}
		chunk := cur
		cur = make([]byte, 0, c.max) // fresh backing array; old chunk is safe to hand off
		h = 0
		return fn(chunk)
	}

	for {
		b, err := br.ReadByte()
		if err != nil {
			if err == io.EOF {
				return flush() // emit the final (possibly short) chunk
			}
			return err
		}

		cur = append(cur, b)
		h = (h << 1) + gearTable[b] // the Gear roll: shift left, add this byte's value

		// Cut if we've hit a content-defined boundary past the minimum size,
		// or if we've grown to the maximum allowed size.
		atBoundary := len(cur) >= c.min && (h&c.mask) == 0
		if atBoundary || len(cur) >= c.max {
			if err := flush(); err != nil {
				return err
			}
		}
	}
}