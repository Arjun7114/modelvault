// Package chunker splits a byte stream into chunks. The chunk boundaries
// determine how well de-duplication works: see the fixed-size and
// content-defined implementations in this package.
package chunker

import "io"

// Chunker splits the data read from r into a sequence of chunks, invoking
// fn once per chunk, in order. Using a callback (rather than returning one
// big [][]byte) means we never hold the whole file in memory at once —
// important for backing up multi-gigabyte model artifacts.
//
// If fn returns an error, Split stops and returns that error.
type Chunker interface {
	Split(r io.Reader, fn func(chunk []byte) error) error
}