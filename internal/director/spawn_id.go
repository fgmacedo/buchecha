package director

import (
	"crypto/rand"
	"regexp"
	"time"
)

// crockfordAlphabet is Crockford's Base32 alphabet in lowercase.
// Characters i, l, o, u are excluded to avoid visual ambiguity.
const crockfordAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"

var validSpawnIDRe = regexp.MustCompile(`^[0-9a-z]{16,32}$`)

// NewSpawnID returns a 26-character lowercase ULID: the first 10 characters
// encode the current millisecond timestamp as Crockford base32 (48 bits),
// and the last 16 characters encode 80 random bits from crypto/rand. IDs
// from consecutive calls sort lexicographically in creation order.
func NewSpawnID() string {
	ms := uint64(time.Now().UnixMilli())
	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		panic("director: NewSpawnID: crypto/rand: " + err.Error())
	}
	var out [26]byte
	// Encode 48-bit timestamp as first 10 chars (5 bits each).
	for i := 9; i >= 0; i-- {
		out[i] = crockfordAlphabet[ms&0x1f]
		ms >>= 5
	}
	// Encode 80-bit random as last 16 chars: two rounds of 5 bytes → 8 chars.
	for k := range 2 {
		b := rnd[k*5:]
		v := uint64(b[0])<<32 | uint64(b[1])<<24 | uint64(b[2])<<16 | uint64(b[3])<<8 | uint64(b[4])
		for i := 7; i >= 0; i-- {
			out[10+k*8+i] = crockfordAlphabet[v&0x1f]
			v >>= 5
		}
	}
	return string(out[:])
}

// ValidSpawnID reports whether id is a valid spawn id: 16 to 32 lowercase
// alphanumeric characters matching [0-9a-z]. The range accommodates possible
// future length changes without breaking callers.
func ValidSpawnID(id string) bool {
	return validSpawnIDRe.MatchString(id)
}
