package workspace

import (
	"context"
	"strings"

	"github.com/lib/pq"
)

type vaultOAuthAccount struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Status   string `json:"status"`
}

func vaultMatchEmail(raw string) string {
	email, err := vaultEmail(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(email)
}

func vaultMatchPlatform(platform string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "claude" {
		return "anthropic"
	}
	return platform
}

// Match live metadata on each page read so existing vault entries also pick up
// newly imported OAuth accounts. No secret blobs or account credentials leave
// the server, and no GET request modifies an existing manual account_id.
func (h *Handler) matchVaultOAuthAccounts(ctx context.Context, items []vaultRecord) error {
	if len(items) == 0 {
		return nil
	}
	emails, platforms := []string{}, []string{}
	for i := range items {
		items[i].OAuthMatch = "none"
		items[i].OAuthAccount = nil
		if email := vaultMatchEmail(items[i].Email); email != "" {
			emails = append(emails, email)
			platforms = append(platforms, vaultMatchPlatform(items[i].Platform))
		}
	}
	if len(emails) == 0 {
		return nil
	}
	// Same primary email precedence as the account-management page. The account
	// name is a fallback only when metadata has no email and the whole name is
	// an email address. Never match auxiliary addresses or arbitrary notes.
	rows, err := h.db.QueryContext(ctx, `
	 SELECT id,name,platform,status,primary_email FROM (
	  SELECT id,name,platform,status,LOWER(BTRIM(COALESCE(
	   NULLIF(BTRIM(extra->>'email_address'),''),
	   NULLIF(BTRIM(extra->>'email'),''),
	   NULLIF(BTRIM(credentials->>'email'),''), name))) AS primary_email
	  FROM accounts WHERE type='oauth' AND deleted_at IS NULL AND platform=ANY($1::text[])
	 ) candidates WHERE primary_email=ANY($2::text[])`, pq.Array(platforms), pq.Array(emails))
	if err != nil {
		return err
	}
	defer rows.Close()
	matches := make(map[string][]vaultOAuthAccount)
	for rows.Next() {
		var item vaultOAuthAccount
		var email string
		if err := rows.Scan(&item.ID, &item.Name, &item.Platform, &item.Status, &email); err != nil {
			return err
		}
		email = vaultMatchEmail(email)
		if email == "" {
			continue
		}
		key := vaultMatchPlatform(item.Platform) + "\x00" + email
		matches[key] = append(matches[key], item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range items {
		key := vaultMatchPlatform(items[i].Platform) + "\x00" + vaultMatchEmail(items[i].Email)
		switch found := matches[key]; len(found) {
		case 0:
		case 1:
			account := found[0]
			items[i].OAuthAccount = &account
			items[i].OAuthMatch = "matched"
		default:
			items[i].OAuthMatch = "ambiguous"
		}
	}
	return nil
}
