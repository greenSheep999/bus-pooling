// Package secrets 加解密敏感字段（AES-256-GCM）。
//
// 用在哪：vendor 凭证 / 乘客号池 admin key / webhook 签名密钥 —— 这些必须能取回明文
// 才能用，所以是**可逆加密**，不是 hash（密码走 Argon2id，不用这里）。
//
// 主密钥从环境变量来（CLAUDE.md §7.1），永不落库也不进 yaml。
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// KeySize AES-256 主密钥字节数。env 里是它的 hex（64 个字符）。
const KeySize = 32

var (
	ErrNoKey         = errors.New("secrets: 主密钥为空")
	ErrBadKeySize    = errors.New("secrets: 主密钥必须是 32 字节（64 位 hex）")
	ErrCiphertextBad = errors.New("secrets: 密文损坏或主密钥不对")
)

type Cipher struct {
	aead cipher.AEAD
}

// New 从 hex 主密钥建 Cipher。
func New(masterKeyHex string) (*Cipher, error) {
	if masterKeyHex == "" {
		return nil, ErrNoKey
	}
	key, err := hex.DecodeString(masterKeyHex)
	if err != nil {
		return nil, fmt.Errorf("secrets: 主密钥不是合法 hex: %w", err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w（当前 %d 字节）", ErrBadKeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: 建 AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: 建 GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt 返回 nonce || ciphertext，可直接存 BLOB。
//
// 每次调用都用新的随机 nonce —— 同样的明文加密两次得到不同密文，这是正确的
// （GCM 复用 nonce 会泄露明文异或值）。所以**别拿密文做去重或等值比较**。
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secrets: 生成 nonce: %w", err)
	}
	// Seal 的第一个参数是 dst，把 nonce 传进去让密文直接追加在它后面
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (c *Cipher) Decrypt(blob []byte) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(blob) < ns {
		return nil, ErrCiphertextBad
	}
	nonce, ct := blob[:ns], blob[ns:]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		// 不把底层错误往外带 —— 区分"密钥错"和"密文被改"对攻击者是有用信息
		return nil, ErrCiphertextBad
	}
	return plain, nil
}

// EncryptString / DecryptString 是字符串场景的便捷包装。
func (c *Cipher) EncryptString(s string) ([]byte, error) { return c.Encrypt([]byte(s)) }

func (c *Cipher) DecryptString(blob []byte) (string, error) {
	b, err := c.Decrypt(blob)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// GenerateKeyHex 生成一个新的主密钥（部署时用一次，存进 env）。
func GenerateKeyHex() (string, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return "", fmt.Errorf("secrets: 生成主密钥: %w", err)
	}
	return hex.EncodeToString(key), nil
}
