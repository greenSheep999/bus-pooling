# internal/secrets

敏感字段的可逆加密（AES-256-GCM）。

**主要类型**：`Cipher`（`Encrypt` / `Decrypt` + `*String` 变体）。

**用在哪**：vendor 凭证 / 乘客号池 admin key / webhook 签名密钥 —— 这些要能取回明文才能用。
**密码不走这里**（那是 Argon2id 单向 hash）。

**注意**：每次加密用新随机 nonce，同一明文两次加密得到不同密文 —— **别拿密文做去重或等值比较**。
主密钥从 `BP_MASTER_KEY` 读（`bus-pooling genkey` 生成），永不落库。
