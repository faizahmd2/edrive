package cryptomator

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"strings"
	"testing"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 4493 section 4.
func TestCMAC(t *testing.T) {
	block, _ := aes.NewCipher(unhex(t, "2b7e1516 28aed2a6 abf71588 09cf4f3c"))
	cases := []struct{ msg, want string }{
		{"", "bb1d6929 e9593728 7fa37d12 9b756746"},
		{"6bc1bee2 2e409f96 e93d7e11 7393172a", "070a16b4 6b4d4144 f79bdd9d d04a287c"},
		{"6bc1bee2 2e409f96 e93d7e11 7393172a ae2d8a57 1e03ac9c 9eb76fac 45af8e51 30c81c46 a35ce411", "dfa66747 de9ae630 30ca3261 1497c827"},
	}
	for _, c := range cases {
		if got := cmac(block, unhex(t, c.msg)); !bytes.Equal(got, unhex(t, c.want)) {
			t.Errorf("cmac(%q) = %x", c.msg, got)
		}
	}
}

// RFC 5297 appendix A.1 and A.2.
func TestSIV(t *testing.T) {
	cases := []struct {
		key   string
		ad    []string
		plain string
		out   string
	}{
		{
			key:   "fffefdfc fbfaf9f8 f7f6f5f4 f3f2f1f0 f0f1f2f3 f4f5f6f7 f8f9fafb fcfdfeff",
			ad:    []string{"10111213 14151617 18191a1b 1c1d1e1f 20212223 24252627"},
			plain: "11223344 55667788 99aabbcc ddee",
			out:   "85632d07 c6e8f37f 950acd32 0a2ecc93 40c02b96 90c4dc04 daef7f6a fe5c",
		},
		{
			key: "7f7e7d7c 7b7a7978 77767574 73727170 40414243 44454647 48494a4b 4c4d4e4f",
			ad: []string{
				"00112233 44556677 8899aabb ccddeeff deaddada deaddada ffeeddcc bbaa9988 77665544 33221100",
				"10203040 50607080 90a0",
				"09f91102 9d74e35b d84156c5 635688c0",
			},
			plain: "74686973 20697320 736f6d65 20706c61 696e7465 78742074 6f20656e 63727970 74207573 696e6720 5349562d 414553",
			out:   "7bdb6e3b 432667eb 06f4d14b ff2fbd0f cb900f2f ddbe4043 26601965 c889bf17 dba77ceb 094fa663 b7a3f748 ba8af829 ea64ad54 4a272e9c 485b62a3 fd5c0d",
		},
	}
	for i, c := range cases {
		key := unhex(t, c.key)
		var ad [][]byte
		for _, a := range c.ad {
			ad = append(ad, unhex(t, a))
		}
		got, err := sivEncrypt(key[:16], key[16:], unhex(t, c.plain), ad...)
		if err != nil || !bytes.Equal(got, unhex(t, c.out)) {
			t.Fatalf("case %d: encrypt = %x, %v", i, got, err)
		}
		plain, err := sivDecrypt(key[:16], key[16:], got, ad...)
		if err != nil || !bytes.Equal(plain, unhex(t, c.plain)) {
			t.Fatalf("case %d: decrypt = %x, %v", i, plain, err)
		}
		got[20] ^= 1
		if _, err := sivDecrypt(key[:16], key[16:], got, ad...); err == nil {
			t.Fatalf("case %d: a changed byte must fail", i)
		}
	}
}
