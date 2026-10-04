package cryptomator

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/scrypt"
)

// RFC 3394 section 4.6: wrap 256 bits of key data with a 256-bit KEK.
func TestAESKeyUnwrapRFC3394(t *testing.T) {
	kek, _ := hex.DecodeString("000102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F")
	wrapped, _ := hex.DecodeString("28C9F404C4B810F4CBCCB35CFB87F8263F5786E2D80ED326CBC7F0E71A99F43BFB988B9B7A02DD21")
	want, _ := hex.DecodeString("00112233445566778899AABBCCDDEEFF000102030405060708090A0B0C0D0E0F")

	got, err := aesKeyUnwrap(kek, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unwrap = %x, want %x", got, want)
	}

	wrapped[10] ^= 1
	if _, err := aesKeyUnwrap(kek, wrapped); err == nil {
		t.Fatal("tampered input must fail")
	}
}

func TestVerifyPassword(t *testing.T) {
	dir := t.TempDir()
	salt := []byte("0123456789abcdef")
	kek, err := scrypt.Key([]byte("correct horse"), salt, 1024, 8, 1, 32)
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, 32)
	mk := map[string]any{
		"version":          999,
		"scryptSalt":       base64.StdEncoding.EncodeToString(salt),
		"scryptCostParam":  1024,
		"scryptBlockSize":  8,
		"primaryMasterKey": base64.StdEncoding.EncodeToString(aesKeyWrap(t, kek, key)),
	}
	data, _ := json.Marshal(mk)
	if err := os.WriteFile(filepath.Join(dir, "masterkey.cryptomator"), data, 0600); err != nil {
		t.Fatal(err)
	}

	if err := VerifyPassword(dir, []byte("correct horse")); err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	if err := VerifyPassword(dir, []byte("wrong")); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: got %v", err)
	}
}

// aesKeyWrap is RFC 3394 section 2.2.1, used only to build test fixtures.
func aesKeyWrap(t *testing.T, kek, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(kek)
	if err != nil {
		t.Fatal(err)
	}
	n := len(key) / 8
	a := append([]byte(nil), defaultIV...)
	r := append([]byte(nil), key...)
	buf := make([]byte, 16)
	for j := 0; j <= 5; j++ {
		for i := 1; i <= n; i++ {
			copy(buf[:8], a)
			copy(buf[8:], r[(i-1)*8:i*8])
			block.Encrypt(buf, buf)
			binary.BigEndian.PutUint64(a, binary.BigEndian.Uint64(buf[:8])^uint64(n*j+i))
			copy(r[(i-1)*8:i*8], buf[8:])
		}
	}
	return append(a, r...)
}
