package secrets

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key, err := GenerateKeyHex()
	if err != nil {
		t.Fatalf("生成密钥: %v", err)
	}
	c, err := New(key)
	if err != nil {
		t.Fatalf("建 cipher: %v", err)
	}
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)

	cases := []struct {
		name string
		in   []byte
	}{
		{"普通字符串", []byte("kiro_admin_key_abc123")},
		{"空", []byte("")},
		{"中文", []byte("乘客号池的管理密钥")},
		{"二进制含 0", []byte{0x00, 0x01, 0xff, 0x00}},
		{"长文本", bytes.Repeat([]byte("x"), 8192)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blob, err := c.Encrypt(tc.in)
			if err != nil {
				t.Fatalf("加密: %v", err)
			}
			got, err := c.Decrypt(blob)
			if err != nil {
				t.Fatalf("解密: %v", err)
			}
			if !bytes.Equal(got, tc.in) {
				t.Fatalf("往返不一致: 得到 %q want %q", got, tc.in)
			}
		})
	}
}

// 同一明文两次加密必须得到不同密文（GCM nonce 不复用）。
// 这条也是"别拿密文做去重/等值比较"的依据。
func TestEncryptIsNonDeterministic(t *testing.T) {
	c := newTestCipher(t)
	plain := []byte("same-input")

	a, err := c.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("同一明文两次加密得到相同密文 —— nonce 被复用了")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	c := newTestCipher(t)
	blob, err := c.Encrypt([]byte("sensitive"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("改密文最后一字节", func(t *testing.T) {
		bad := bytes.Clone(blob)
		bad[len(bad)-1] ^= 0xff
		if _, err := c.Decrypt(bad); !errors.Is(err, ErrCiphertextBad) {
			t.Fatalf("篡改后应报 ErrCiphertextBad，得到 %v", err)
		}
	})

	t.Run("改 nonce", func(t *testing.T) {
		bad := bytes.Clone(blob)
		bad[0] ^= 0xff
		if _, err := c.Decrypt(bad); !errors.Is(err, ErrCiphertextBad) {
			t.Fatalf("篡改 nonce 后应报 ErrCiphertextBad，得到 %v", err)
		}
	})

	t.Run("截断", func(t *testing.T) {
		if _, err := c.Decrypt(blob[:5]); !errors.Is(err, ErrCiphertextBad) {
			t.Fatalf("截断后应报 ErrCiphertextBad，得到 %v", err)
		}
	})
}

// 换了主密钥就必须解不开旧密文 —— 否则说明密钥没真正参与运算。
func TestDecryptWithWrongKeyFails(t *testing.T) {
	c1 := newTestCipher(t)
	c2 := newTestCipher(t)

	blob, err := c1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c2.Decrypt(blob); !errors.Is(err, ErrCiphertextBad) {
		t.Fatalf("用别的密钥应解不开，得到 %v", err)
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want error
	}{
		{"空", "", ErrNoKey},
		{"太短", strings.Repeat("ab", 8), ErrBadKeySize},
		{"太长", strings.Repeat("ab", 40), ErrBadKeySize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.key); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}

	t.Run("非 hex", func(t *testing.T) {
		if _, err := New(strings.Repeat("zz", 32)); err == nil {
			t.Fatal("非 hex 主密钥应该报错")
		}
	})
}

func TestStringHelpers(t *testing.T) {
	c := newTestCipher(t)
	const in = "usr-abc123"

	blob, err := c.EncryptString(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.DecryptString(blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != in {
		t.Fatalf("got %q want %q", got, in)
	}
}

func TestGenerateKeyHexLength(t *testing.T) {
	k, err := GenerateKeyHex()
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != KeySize*2 {
		t.Fatalf("主密钥 hex 应为 %d 字符，得到 %d", KeySize*2, len(k))
	}
	// 两次生成不能一样
	k2, _ := GenerateKeyHex()
	if k == k2 {
		t.Fatal("两次生成的主密钥相同")
	}
}
