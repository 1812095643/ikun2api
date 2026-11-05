package service

import (
	"net/mail"
	"strings"
)

// PoolAccountEmailLabel shares one email projection between the user pool and
// its usage records. Only a bare primary email is accepted; labels, notes and
// credentials other than the email field must never become public identifiers.
func PoolAccountEmailLabel(account *Account, snapshot *string) string {
	candidates := make([]string, 0, 5)
	if account != nil {
		emailAddress, _ := account.Extra["email_address"].(string)
		email, _ := account.Extra["email"].(string)
		candidates = append(candidates, emailAddress, email, account.GetCredential("email"), account.Name)
	}
	if snapshot != nil {
		candidates = append(candidates, *snapshot)
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.ContainsAny(candidate, "\r\n") {
			continue
		}
		address, err := mail.ParseAddress(candidate)
		if err == nil && address.Name == "" && address.Address == candidate {
			return MaskAccountName(candidate)
		}
	}
	return "—"
}
