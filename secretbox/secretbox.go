package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
)

const KeySize = 32

var (
	ErrInvalidKeyring    = errors.New("secretbox: invalid keyring")
	ErrUnknownKey        = errors.New("secretbox: unknown key version")
	ErrInvalidCiphertext = errors.New("secretbox: invalid ciphertext")
	ErrAuthentication    = errors.New("secretbox: authentication failed")
)

// Key is one versioned AES-256 key. Material must contain exactly 32 bytes.
type Key struct {
	Version  string
	Material []byte
}

// Sealed is the value persisted by callers. Ciphertext contains a random nonce
// prefix followed by the GCM ciphertext and authentication tag.
type Sealed struct {
	KeyVersion string
	Ciphertext []byte
}

// Box is an immutable versioned AES-GCM keyring. The first key supplied to New
// is the primary write key; all supplied keys may decrypt matching versions.
type Box struct {
	primary string
	aeads   map[string]cipher.AEAD
	random  io.Reader
}

// New validates keys and creates a versioned AES-256-GCM keyring.
func New(keys ...Key) (*Box, error) {
	if len(keys) == 0 {
		return nil, ErrInvalidKeyring
	}
	aeads := make(map[string]cipher.AEAD, len(keys))
	for _, key := range keys {
		version := strings.TrimSpace(key.Version)
		if version == "" || len(version) > 32 || len(key.Material) != KeySize {
			return nil, ErrInvalidKeyring
		}
		if _, exists := aeads[version]; exists {
			return nil, ErrInvalidKeyring
		}
		block, err := aes.NewCipher(append([]byte(nil), key.Material...))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidKeyring, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidKeyring, err)
		}
		aeads[version] = aead
	}
	return &Box{primary: strings.TrimSpace(keys[0].Version), aeads: aeads, random: rand.Reader}, nil
}

// Encrypt seals plaintext with the primary key and optional associated data.
func (b *Box) Encrypt(plaintext, associatedData []byte) (Sealed, error) {
	if b == nil {
		return Sealed{}, ErrInvalidKeyring
	}
	aead, ok := b.aeads[b.primary]
	if !ok || b.random == nil {
		return Sealed{}, ErrInvalidKeyring
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(b.random, nonce); err != nil {
		return Sealed{}, fmt.Errorf("secretbox: generate nonce: %w", err)
	}
	out := make([]byte, 0, len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, associatedData)
	return Sealed{KeyVersion: b.primary, Ciphertext: out}, nil
}

// Decrypt authenticates and opens a value using its recorded key version.
func (b *Box) Decrypt(sealed Sealed, associatedData []byte) ([]byte, error) {
	if b == nil {
		return nil, ErrInvalidKeyring
	}
	aead, ok := b.aeads[strings.TrimSpace(sealed.KeyVersion)]
	if !ok {
		return nil, ErrUnknownKey
	}
	if len(sealed.Ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrInvalidCiphertext
	}
	nonce := sealed.Ciphertext[:aead.NonceSize()]
	ciphertext := sealed.Ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}
