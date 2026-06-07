/*
 * MinIO Cloud Storage, (C) 2018 MinIO, Inc.
 * Modifications and additions (C) 2025-2026 soulteary, https://github.com/soulteary/otterio
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package madmin

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"github.com/secure-io/sio-go"
	"github.com/secure-io/sio-go/sioutil"
	"github.com/soulteary/otterio/pkg/argon2"
)

var idKey func([]byte, []byte, []byte, []byte, uint32) []byte

func init() {
	idKey = argon2.NewIDKey(1, 64*1024, 4)
}

// SetIDKeyForTesting overrides the package-level Argon2id key derivation
// function. This is intended for tests only: the default Argon2id parameters
// (1 iteration, 64 MiB, 4 threads) are deliberately expensive and become
// unbearably slow under -race because the Go race detector instruments every
// memory access in the 64 MiB working set, which drastically slows the
// large-loop XOR kernel and was observed to wedge the cmd/ test suite past
// its 20-minute deadline. Tests that exercise saveServerConfig / DecryptData
// indirectly (e.g. via initAllSubsystems) should call this with a cheap key
// derivation function (see SetCheapTestIDKey).
func SetIDKeyForTesting(fn func([]byte, []byte, []byte, []byte, uint32) []byte) (restore func()) {
	prev := idKey
	idKey = fn
	return func() { idKey = prev }
}

// SetCheapTestIDKey installs a deterministic, non-cryptographic key derivation
// function suitable only for tests. It expands the password+salt into a
// keyLen-byte key by repeating SHA-256, which is roughly five orders of
// magnitude faster than the production Argon2id parameters under -race.
func SetCheapTestIDKey() (restore func()) {
	return SetIDKeyForTesting(cheapTestIDKey)
}

// EncryptData encrypts the data with an unique key
// derived from password using the Argon2id PBKDF.
//
// The returned ciphertext data consists of:
//
//	salt | AEAD ID | nonce | encrypted data
//	 32      1         8      ~ len(data)
func EncryptData(password string, data []byte) ([]byte, error) {
	salt := sioutil.MustRandom(32)

	// Derive an unique 256 bit key from the password and the random salt.
	key := idKey([]byte(password), salt, nil, nil, 32)

	var (
		id     byte
		err    error
		stream *sio.Stream
	)
	if useAES() { // Only use AES-GCM if we can use an optimized implementation
		id = aesGcm
		stream, err = sio.AES_256_GCM.Stream(key)
	} else {
		id = c20p1305
		stream, err = sio.ChaCha20Poly1305.Stream(key)
	}
	if err != nil {
		return nil, err
	}
	nonce := sioutil.MustRandom(stream.NonceSize())

	// ciphertext = salt || AEAD ID | nonce | encrypted data
	cLen := int64(len(salt)+1+len(nonce)+len(data)) + stream.Overhead(int64(len(data)))
	ciphertext := bytes.NewBuffer(make([]byte, 0, cLen)) // pre-alloc correct length

	// Prefix the ciphertext with salt, AEAD ID and nonce
	ciphertext.Write(salt)
	ciphertext.WriteByte(id)
	ciphertext.Write(nonce)

	w := stream.EncryptWriter(ciphertext, nonce, nil)
	if _, err = w.Write(data); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return ciphertext.Bytes(), nil
}

// ErrMaliciousData indicates that the stream cannot be
// decrypted by provided credentials.
var ErrMaliciousData = sio.NotAuthentic

// DecryptData decrypts the data with the key derived
// from the salt (part of data) and the password using
// the PBKDF used in EncryptData. DecryptData returns
// the decrypted plaintext on success.
//
// The data must be a valid ciphertext produced by
// EncryptData. Otherwise, the decryption will fail.
func DecryptData(password string, data io.Reader) ([]byte, error) {
	var (
		salt  [32]byte
		id    [1]byte
		nonce [8]byte // This depends on the AEAD but both used ciphers have the same nonce length.
	)

	if _, err := io.ReadFull(data, salt[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(data, id[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(data, nonce[:]); err != nil {
		return nil, err
	}

	key := idKey([]byte(password), salt[:], nil, nil, 32)
	var (
		err    error
		stream *sio.Stream
	)
	switch id[0] {
	case aesGcm:
		stream, err = sio.AES_256_GCM.Stream(key)
	case c20p1305:
		stream, err = sio.ChaCha20Poly1305.Stream(key)
	default:
		err = errors.New("madmin: invalid AEAD algorithm ID")
	}
	if err != nil {
		return nil, err
	}

	enBytes, err := io.ReadAll(stream.DecryptReader(data, nonce[:], nil))
	if err != nil {
		if err == sio.NotAuthentic {
			return enBytes, ErrMaliciousData
		}
	}
	return enBytes, err
}

const (
	aesGcm   = 0x00
	c20p1305 = 0x01
)

// cheapTestIDKey is a deterministic key derivation function used only by
// SetCheapTestIDKey. It is intentionally NOT cryptographically strong: callers
// outside of tests must keep using the Argon2id-based default. The keyLen
// parameter is a uint32 to match the Argon2 IDKey signature; only the lower
// 32 bits are used.
func cheapTestIDKey(password, salt, _, _ []byte, keyLen uint32) []byte {
	if keyLen == 0 {
		return nil
	}
	out := make([]byte, 0, keyLen)
	var counter uint32
	for uint32(len(out)) < keyLen {
		var ctr [4]byte
		binary.BigEndian.PutUint32(ctr[:], counter)
		h := sha256.New()
		_, _ = h.Write(password)
		_, _ = h.Write(salt)
		_, _ = h.Write(ctr[:])
		out = h.Sum(out)
		counter++
	}
	return out[:keyLen]
}
