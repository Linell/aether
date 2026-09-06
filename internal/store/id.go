package store

import (
	"crypto/rand"
	"math/big"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func NewID() string {
	var b [16]byte
	putMillis(b[:6], time.Now().UnixMilli())
	if _, err := rand.Read(b[6:]); err != nil {
		panic("store: crypto/rand unavailable: " + err.Error())
	}
	return base32(b[:], 26)
}

func putMillis(dst []byte, ms int64) {
	for i := range dst {
		dst[i] = byte(ms >> (8 * (len(dst) - 1 - i)))
	}
}

func base32(src []byte, width int) string {
	n := new(big.Int).SetBytes(src)
	base := big.NewInt(32)
	mod := new(big.Int)
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		n.DivMod(n, base, mod)
		out[i] = crockford[mod.Int64()]
	}
	return string(out)
}
