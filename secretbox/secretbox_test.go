package secretbox

import (
	"bytes"
	"errors"
	"testing"
)

func TestBoxEncryptDecrypt(t *testing.T) {
	box, err := New(Key{Version: "v1", Material: bytes.Repeat([]byte{0x11}, KeySize)})
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := box.Encrypt([]byte("person@example.com"), []byte("tenant:7:email"))
	if err != nil {
		t.Fatal(err)
	}
	if sealed.KeyVersion != "v1" {
		t.Fatalf("KeyVersion = %q, want v1", sealed.KeyVersion)
	}
	if bytes.Contains(sealed.Ciphertext, []byte("person@example.com")) {
		t.Fatal("ciphertext contains plaintext")
	}

	plain, err := box.Decrypt(sealed, []byte("tenant:7:email"))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "person@example.com" {
		t.Fatalf("plaintext = %q", plain)
	}
}

func TestBoxRejectsTamperAndWrongAAD(t *testing.T) {
	box, err := New(Key{Version: "v1", Material: bytes.Repeat([]byte{0x22}, KeySize)})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Encrypt([]byte("+8613800138000"), []byte("tenant:7:phone"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := Sealed{KeyVersion: sealed.KeyVersion, Ciphertext: append([]byte(nil), sealed.Ciphertext...)}
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 0xff
	if _, err := box.Decrypt(tampered, []byte("tenant:7:phone")); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("tampered decrypt error = %v, want ErrAuthentication", err)
	}
	if _, err := box.Decrypt(sealed, []byte("tenant:8:phone")); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong AAD decrypt error = %v, want ErrAuthentication", err)
	}
}

func TestBoxRotationReadsOldAndWritesPrimary(t *testing.T) {
	v1 := Key{Version: "v1", Material: bytes.Repeat([]byte{0x33}, KeySize)}
	v2 := Key{Version: "v2", Material: bytes.Repeat([]byte{0x44}, KeySize)}
	oldBox, err := New(v1)
	if err != nil {
		t.Fatal(err)
	}
	old, err := oldBox.Encrypt([]byte("old-value"), nil)
	if err != nil {
		t.Fatal(err)
	}

	rotated, err := New(v2, v1)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := rotated.Decrypt(old, nil)
	if err != nil || string(plain) != "old-value" {
		t.Fatalf("decrypt old = %q, %v", plain, err)
	}
	fresh, err := rotated.Encrypt([]byte("new-value"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.KeyVersion != "v2" {
		t.Fatalf("new KeyVersion = %q, want v2", fresh.KeyVersion)
	}
}

func TestNewValidatesKeyring(t *testing.T) {
	tests := []struct {
		name string
		keys []Key
	}{
		{name: "empty"},
		{name: "missing version", keys: []Key{{Material: bytes.Repeat([]byte{1}, KeySize)}}},
		{name: "wrong key size", keys: []Key{{Version: "v1", Material: []byte("short")}}},
		{name: "duplicate version", keys: []Key{
			{Version: "v1", Material: bytes.Repeat([]byte{1}, KeySize)},
			{Version: "v1", Material: bytes.Repeat([]byte{2}, KeySize)},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.keys...); !errors.Is(err, ErrInvalidKeyring) {
				t.Fatalf("New error = %v, want ErrInvalidKeyring", err)
			}
		})
	}
}

func TestDecryptRejectsUnknownVersionAndMalformedCiphertext(t *testing.T) {
	box, err := New(Key{Version: "v1", Material: bytes.Repeat([]byte{0x55}, KeySize)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.Decrypt(Sealed{KeyVersion: "v2", Ciphertext: []byte("value")}, nil); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown version error = %v, want ErrUnknownKey", err)
	}
	if _, err := box.Decrypt(Sealed{KeyVersion: "v1", Ciphertext: []byte("short")}, nil); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("short ciphertext error = %v, want ErrInvalidCiphertext", err)
	}
}
