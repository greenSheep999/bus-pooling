package main

// seed_credplain · 手动往 credential_plaintext 表塞一份加密明文
//
// 用途: 手动 BatchImport 到 housepool 后端 的号 · bus-pooling 侧从来不知道明文 ·
// 用这个 CLI 补一下 · 让 push_pool / handoff 能拿到真明文 · 不走 placeholder
//
// 用法(vps22 上 · docker compose run):
//   docker compose run --rm --entrypoint '/app/bus-pooling' app seed-credplain \
//     -credential-id cred-manual-k2 -auth-method api_key -kiro-api-key ksk_xxx -email leedx2011@..

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/credplain"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/secrets"
)

func runSeedCredplain(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("seed-credplain", flag.ContinueOnError)
	credID := fs.String("credential-id", "", "credential_ledger.id (cred-manual-kN)")
	authMethod := fs.String("auth-method", "", "refresh_token | api_key | bearer")
	refresh := fs.String("refresh-token", "", "AuthRefreshToken 用")
	accessTok := fs.String("access-token", "", "AuthBearer 用")
	kiroKey := fs.String("kiro-api-key", "", "AuthAPIKey 用(ksk_xxx)")
	email := fs.String("email", "", "邮箱 · 可选")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *credID == "" || *authMethod == "" {
		return errors.New("需要: -credential-id + -auth-method")
	}

	cipher, err := secrets.New(cfg.Secrets.MasterKey)
	if err != nil {
		return fmt.Errorf("secrets.New: %w", err)
	}

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	store := credplain.New(database.DB, cipher)
	if err := store.Save(ctx, credplain.SaveInput{
		CredentialID: *credID,
		AuthMethod:   credplain.AuthMethod(*authMethod),
		RefreshToken: *refresh,
		AccessToken:  *accessTok,
		KiroAPIKey:   *kiroKey,
		Email:        *email,
	}); err != nil {
		return fmt.Errorf("Save: %w", err)
	}
	fmt.Printf("done · credential_id=%s auth=%s\n", *credID, *authMethod)
	return nil
}
