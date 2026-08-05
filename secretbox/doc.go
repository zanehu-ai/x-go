// Package secretbox encrypts small application secrets with AES-256-GCM.
//
// Callers persist the returned key version beside the ciphertext. During a
// rotation, construct a Box with the new key first and the old read keys after
// it: encryption always uses the first key while decryption selects by version.
// Associated data is authenticated but not encrypted and should bind ciphertext
// to its tenant, record, and field context.
package secretbox
