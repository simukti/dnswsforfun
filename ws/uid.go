package ws

import (
	cryptoRand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/rand"
)

const (
	defaultUIDLen      = 20
	defaultUIDCharlist = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_-"
)

// uidGenerator simple random string generator for demo purpose.
type uidGenerator struct{}

func newUIDGenerator() *uidGenerator {
	var s [16]byte
	if _, err := cryptoRand.Read(s[:]); err != nil {
		panic(fmt.Sprintf("could not get random bytes from cryto/rand: '%s'", err.Error()))
	}
	// seed math/rand with 16 random bytes from crypto/rand to make sure rand.Seed is not 1.
	rand.Seed(int64(binary.LittleEndian.Uint64(s[:])))
	return &uidGenerator{}
}

func (u *uidGenerator) UID() string {
	var random, uid [defaultUIDLen]byte
	if _, err := rand.Read(random[:]); err != nil { //nolint:gosec
		panic(fmt.Sprintf("random read error from math/rand: '%s'", err.Error()))
	}
	for i := 0; i < defaultUIDLen; i++ {
		uid[i] = defaultUIDCharlist[random[i]&62]
	}
	return string(uid[:])
}
