package cryptomator

import (
	"crypto/aes"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/scrypt"
	"golang.org/x/text/unicode/norm"
)

// ErrWrongPassword means the password does not open this vault.
var ErrWrongPassword = errors.New("wrong vault password")

type masterkeyFile struct {
	ScryptSalt       string `json:"scryptSalt"`
	ScryptCostParam  int    `json:"scryptCostParam"`
	ScryptBlockSize  int    `json:"scryptBlockSize"`
	PrimaryMasterKey string `json:"primaryMasterKey"`
}

// VerifyPassword checks a password against masterkey.cryptomator without
// mounting anything. Cryptomator derives a key-encryption key with scrypt and
// wraps the vault key with AES key wrap (RFC 3394); unwrapping only succeeds
// with the right password.
func VerifyPassword(vaultPath string, password []byte) error {
	data, err := os.ReadFile(filepath.Join(vaultPath, "masterkey.cryptomator"))
	if err != nil {
		return fmt.Errorf("read vault masterkey: %w", err)
	}
	var mk masterkeyFile
	if err := json.Unmarshal(data, &mk); err != nil {
		return fmt.Errorf("vault masterkey is not valid JSON: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(mk.ScryptSalt)
	if err != nil {
		return fmt.Errorf("vault masterkey salt is invalid")
	}
	wrapped, err := base64.StdEncoding.DecodeString(mk.PrimaryMasterKey)
	if err != nil {
		return fmt.Errorf("vault masterkey is invalid")
	}
	if mk.ScryptCostParam <= 1 || mk.ScryptBlockSize <= 0 {
		return fmt.Errorf("vault masterkey has unsupported scrypt parameters")
	}

	normalized := norm.NFC.Bytes(password)
	kek, err := scrypt.Key(normalized, salt, mk.ScryptCostParam, mk.ScryptBlockSize, 1, 32)
	if err != nil {
		return err
	}
	if _, err := aesKeyUnwrap(kek, wrapped); err != nil {
		return ErrWrongPassword
	}
	return nil
}

var defaultIV = []byte{0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6, 0xA6}

// aesKeyUnwrap implements RFC 3394 section 2.2.2.
func aesKeyUnwrap(kek, wrapped []byte) ([]byte, error) {
	if len(wrapped)%8 != 0 || len(wrapped) < 24 {
		return nil, errors.New("invalid wrapped key length")
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	n := len(wrapped)/8 - 1
	a := make([]byte, 8)
	copy(a, wrapped[:8])
	r := make([]byte, n*8)
	copy(r, wrapped[8:])

	buf := make([]byte, 16)
	for j := 5; j >= 0; j-- {
		for i := n; i >= 1; i-- {
			t := uint64(n*j + i)
			binary.BigEndian.PutUint64(buf[:8], binary.BigEndian.Uint64(a)^t)
			copy(buf[8:], r[(i-1)*8:i*8])
			block.Decrypt(buf, buf)
			copy(a, buf[:8])
			copy(r[(i-1)*8:i*8], buf[8:])
		}
	}
	if subtle.ConstantTimeCompare(a, defaultIV) != 1 {
		return nil, errors.New("integrity check failed")
	}
	return r, nil
}
