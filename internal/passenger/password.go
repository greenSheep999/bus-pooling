package passenger

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id 参数（docs/03-modules.md §334 定的，别随便改 —— 改了旧 hash 仍能验，
// 因为参数编码在 hash 串里，但新旧强度会不一致）。
const (
	argonMemory      = 64 * 1024 // 64 MiB
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLen     = 16
	argonKeyLen      = 32
)

var (
	ErrBadHashFormat  = errors.New("passenger: 密码 hash 格式不对")
	ErrWrongPassword  = errors.New("passenger: 密码不对")
	ErrUnsupportedAlg = errors.New("passenger: 不支持的密码算法")
)

// HashPassword 返回 PHC 格式串：$argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
//
// 参数编码在串里，所以将来调参数不会让老用户登不进来。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("passenger: 生成 salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt,
		argonIterations, argonMemory, argonParallelism, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 用 hash 串里编码的参数重算，常数时间比较。
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 || parts[0] != "" {
		return ErrBadHashFormat
	}
	if parts[1] != "argon2id" {
		return ErrUnsupportedAlg
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return ErrBadHashFormat
	}
	if version != argon2.Version {
		return ErrUnsupportedAlg
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return ErrBadHashFormat
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrBadHashFormat
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrBadHashFormat
	}

	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))

	// 常数时间比较 —— 用 == 会因短路泄露前缀匹配长度
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrWrongPassword
	}
	return nil
}
