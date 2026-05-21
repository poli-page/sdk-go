// Package uuid generates RFC 4122 §4.4 version-4 UUIDs from crypto/rand.
//
// Used by the SDK to populate the auto-generated Idempotency-Key header on
// POST requests when the caller does not supply one explicitly.
package uuid

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns a fresh RFC 4122 §4.4 UUIDv4 in the canonical 8-4-4-4-12 hex
// form. Panics if crypto/rand returns an error — same posture as
// the standard library's crypto packages when entropy is unavailable.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("uuid: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}
