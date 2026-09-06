package store

import (
	"crypto/rand"
	"encoding/base32"
	"time"
)

var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

func NewID() string {
	var b [16]byte
	putMillis(b[:6], time.Now().UnixMilli())
	if _, err := rand.Read(b[6:]); err != nil {
		panic("store: crypto/rand unavailable: " + err.Error())
	}
	return crockford.EncodeToString(b[:])
}

func putMillis(dst []byte, ms int64) {
	for i := range dst {
		dst[i] = byte(ms >> (8 * (len(dst) - 1 - i)))
	}
}
