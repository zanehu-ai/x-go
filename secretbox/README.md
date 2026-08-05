# secretbox

`secretbox` encrypts small application secrets with AES-256-GCM. It keeps key
rotation explicit: the first key is used for new writes, while every supplied
version remains available for reads.

```go
box, err := secretbox.New(
    secretbox.Key{Version: "v2", Material: currentKey},
    secretbox.Key{Version: "v1", Material: previousKey},
)
if err != nil {
    return err
}

sealed, err := box.Encrypt(contact, []byte("tenant:42:email"))
if err != nil {
    return err
}
// Persist sealed.Ciphertext and sealed.KeyVersion in separate columns.

plain, err := box.Decrypt(sealed, []byte("tenant:42:email"))
if err != nil {
    return err
}
_ = plain // Pass the authenticated plaintext to the owning workflow.
```

Key material must be exactly 32 bytes and come from a managed secret store.
Never reuse contact-deduplication or session-signing keys as encryption keys.
Associated data must be reconstructed identically for decryption and should
bind a value to its tenant and field context. Ciphertext includes a random
nonce prefix; callers must not supply or reuse nonces.
