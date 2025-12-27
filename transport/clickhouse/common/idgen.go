package common

import (
	"crypto/rand"
	"encoding/hex"
)

func ShortID() string {
	var r [8]byte
	rand.Read(r[:])
	return hex.EncodeToString(r[:])
}
