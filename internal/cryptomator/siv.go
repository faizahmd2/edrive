package cryptomator

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"errors"
)

// AES-SIV (RFC 5297), which Cryptomator uses for file names and directory
// ids. macKey drives S2V (AES-CMAC, RFC 4493); encKey drives AES-CTR. The
// RFC writes the key as macKey‖encKey.

var errSIV = errors.New("authentication failed")

// sivEncrypt returns V‖C.
func sivEncrypt(macKey, encKey, plaintext []byte, ad ...[]byte) ([]byte, error) {
	v, err := s2v(macKey, plaintext, ad...)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 16+len(plaintext))
	copy(out, v)
	if err := sivCTR(encKey, v, out[16:], plaintext); err != nil {
		return nil, err
	}
	return out, nil
}

// sivDecrypt checks and opens V‖C.
func sivDecrypt(macKey, encKey, ciphertext []byte, ad ...[]byte) ([]byte, error) {
	if len(ciphertext) < 16 {
		return nil, errSIV
	}
	v, c := ciphertext[:16], ciphertext[16:]
	plain := make([]byte, len(c))
	if err := sivCTR(encKey, v, plain, c); err != nil {
		return nil, err
	}
	t, err := s2v(macKey, plain, ad...)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(t, v) != 1 {
		return nil, errSIV
	}
	return plain, nil
}

// sivCTR runs AES-CTR from V with bits 31 and 63 cleared (RFC 5297 2.6).
func sivCTR(key, v, dst, src []byte) error {
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	q := append([]byte(nil), v...)
	q[8] &= 0x7f
	q[12] &= 0x7f
	cipher.NewCTR(block, q).XORKeyStream(dst, src)
	return nil
}

// s2v is RFC 5297 2.4.
func s2v(key, plaintext []byte, ad ...[]byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	d := cmac(block, make([]byte, 16))
	for _, s := range ad {
		d = xor16(dbl(d), cmac(block, s))
	}
	var t []byte
	if len(plaintext) >= 16 {
		t = append([]byte(nil), plaintext...)
		tail := t[len(t)-16:]
		copy(tail, xor16(tail, d))
	} else {
		padded := make([]byte, 16)
		copy(padded, plaintext)
		padded[len(plaintext)] = 0x80
		t = xor16(dbl(d), padded)
	}
	return cmac(block, t), nil
}

// cmac is AES-CMAC (RFC 4493).
func cmac(block cipher.Block, msg []byte) []byte {
	l := make([]byte, 16)
	block.Encrypt(l, l)
	k1 := dbl(l)
	k2 := dbl(k1)

	n := (len(msg) + 15) / 16
	complete := n > 0 && len(msg)%16 == 0
	if n == 0 {
		n = 1
	}
	last := make([]byte, 16)
	rest := msg[(n-1)*16:]
	if complete {
		copy(last, xor16(rest, k1))
	} else {
		copy(last, rest)
		last[len(rest)] = 0x80
		last = xor16(last, k2)
	}

	x := make([]byte, 16)
	for i := 0; i < n-1; i++ {
		x = xor16(x, msg[i*16:(i+1)*16])
		block.Encrypt(x, x)
	}
	x = xor16(x, last)
	block.Encrypt(x, x)
	return x
}

// dbl multiplies by x in GF(2^128).
func dbl(b []byte) []byte {
	out := make([]byte, 16)
	carry := b[0] >> 7
	for i := 0; i < 15; i++ {
		out[i] = b[i]<<1 | b[i+1]>>7
	}
	out[15] = b[15] << 1
	if carry == 1 {
		out[15] ^= 0x87
	}
	return out
}

func xor16(a, b []byte) []byte {
	out := make([]byte, 16)
	for i := range out {
		out[i] = a[i] ^ b[i]
	}
	return out
}
