package workspace

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type vaultCipher struct {
	aead     cipher.AEAD
	indexKey []byte
	keyID    string
}

func newVaultCipher(key []byte) (*vaultCipher, error) {
	if len(key) != 32 {
		return nil, errors.New("invalid vault key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte("ikun2api/account-vault/index/v1"))
	sum := sha256.Sum256(key)
	return &vaultCipher{aead: a, indexKey: h.Sum(nil), keyID: hex.EncodeToString(sum[:8])}, nil
}
func (v *vaultCipher) index(platform, email string) string {
	h := hmac.New(sha256.New, v.indexKey)
	h.Write([]byte(platform + "\x00" + email))
	return hex.EncodeToString(h.Sum(nil))
}
func (v *vaultCipher) seal(id string, secret vaultSecret) (string, error) {
	raw, err := json.Marshal(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(v.aead.Seal(nonce, nonce, raw, []byte("ikun2api/vault/v1/"+v.keyID+"/"+id+"/payload"))), nil
}
func (v *vaultCipher) open(id, keyID, blob string) (vaultSecret, error) {
	var s vaultSecret
	if keyID != v.keyID {
		return s, errors.New("vault key mismatch")
	}
	data, err := base64.StdEncoding.DecodeString(blob)
	if err != nil || len(data) < v.aead.NonceSize() {
		return s, errors.New("invalid vault ciphertext")
	}
	n := v.aead.NonceSize()
	raw, err := v.aead.Open(nil, data[:n], data[n:], []byte("ikun2api/vault/v1/"+v.keyID+"/"+id+"/payload"))
	if err != nil {
		return s, errors.New("vault authentication failed")
	}
	err = json.Unmarshal(raw, &s)
	return s, err
}
func (h *Handler) vaultKey() (*vaultCipher, error) {
	h.vaultMu.Lock()
	defer h.vaultMu.Unlock()
	if h.vault != nil {
		return h.vault, nil
	}
	path := os.Getenv("ACCOUNT_VAULT_KEY_FILE")
	if path == "" {
		dir := os.Getenv("DATA_DIR")
		if dir == "" {
			dir = "."
			if info, err := os.Stat("data"); err == nil && info.IsDir() {
				dir = "data"
			}
		}
		path = filepath.Join(dir, "account-vault.key")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		var count int
		if e := h.db.QueryRow("SELECT count(*) FROM workspace_account_vault").Scan(&count); e != nil {
			return nil, e
		}
		if count != 0 {
			return nil, errors.New("vault key missing; restore original key")
		}
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(e) {
			data, err = os.ReadFile(path)
		} else if e != nil {
			return nil, e
		} else {
			_, e = f.WriteString(hex.EncodeToString(key))
			if e == nil {
				e = f.Sync()
			}
			closeErr := f.Close()
			if e != nil {
				return nil, e
			}
			if closeErr != nil {
				return nil, closeErr
			}
			data = []byte(hex.EncodeToString(key))
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("vault key file permissions must be 0600")
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, errors.New("invalid vault key file")
	}
	h.vault, err = newVaultCipher(key)
	return h.vault, err
}
