package service

import "unsafe"

// claudeBorrowRawJSON is only for the synchronous privacy-validation pipeline.
// Callers MUST NOT mutate or append to this view. The filters either return the
// original request unchanged, or allocate new storage for transformed objects.
// No borrowed value is cached. Avoid copying large opaque image/tool/thinking
// fields just to validate sibling metadata.
func claudeBorrowRawJSON(raw string) []byte {
	return unsafe.Slice(unsafe.StringData(raw), len(raw))
}
