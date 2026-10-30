package service

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
)

const ReleaseRepository = "1812095643/ikun2api"
const ReleaseSigningPublicKey = "FjAf8K7CSX0LstSKwFGX2sL03GcQ14tUXsbNAAsrGnc="

func verifyReleaseSignature(checksums, signature []byte) error {
	key, err := base64.StdEncoding.DecodeString(ReleaseSigningPublicKey)
	if err != nil {
		return fmt.Errorf("invalid embedded release key")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("invalid release signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(key), checksums, sig) {
		return fmt.Errorf("release signature verification failed")
	}
	return nil
}
