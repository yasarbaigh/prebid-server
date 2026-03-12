package cryptoutil

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// Hardcoded complex key (32 bytes for AES-256)
const AESKey = "a-very-complex-and-secret-key-32"

// Encrypt string to base64 encoded AES-GCM ciphertext
func Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher([]byte(AESKey))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt base64 encoded AES-GCM ciphertext to original plaintext
func Decrypt(cryptoText string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher([]byte(AESKey))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// EncryptCompressed compresses the plaintext with zlib BestSpeed before encrypting.
func EncryptCompressed(plaintext string) (string, error) {
	var b bytes.Buffer
	w, _ := zlib.NewWriterLevel(&b, zlib.BestSpeed)
	w.Write([]byte(plaintext))
	w.Close()

	return Encrypt(b.String())
}

// DecryptCompressed decrypts and then decompresses the data.
func DecryptCompressed(cryptoText string) (string, error) {
	decrypted, err := Decrypt(cryptoText)
	if err != nil {
		return "", err
	}

	r, err := zlib.NewReader(strings.NewReader(decrypted))
	if err != nil {
		return "", err
	}
	defer r.Close()

	var b bytes.Buffer
	if _, err := io.Copy(&b, r); err != nil {
		return "", err
	}
	return b.String(), nil
}
