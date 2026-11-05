package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestVaultOAuthMatchUsesPrimaryEmailAndPlatform(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	items := []vaultRecord{
		{Email: "  Case@Example.test ", Platform: "claude"},
		{Email: "Case@example.test", Platform: "openai"},
		{Email: "missing@example.test", AuxiliaryEmail: "Case@example.test", Platform: "claude"},
		{Email: "duplicate@example.test", Platform: "claude"},
		{Email: "Case+tag@example.test", Platform: "claude"},
	}
	mock.ExpectQuery("SELECT id,name,platform,status,primary_email.*type='oauth' AND deleted_at IS NULL").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "status", "primary_email"}).
		AddRow(1, "Claude source", "anthropic", "disabled", "case@example.test").
		AddRow(2, "OpenAI source", "openai", "active", "case@example.test").
		AddRow(3, "duplicate A", "anthropic", "active", "duplicate@example.test").
		AddRow(4, "duplicate B", "anthropic", "active", "duplicate@example.test"))
	require.NoError(t, (&Handler{db: db}).matchVaultOAuthAccounts(context.Background(), items))
	require.Equal(t, int64(1), items[0].OAuthAccount.ID)
	require.Equal(t, "disabled", items[0].OAuthAccount.Status)
	require.Equal(t, int64(2), items[1].OAuthAccount.ID)
	require.Nil(t, items[2].OAuthAccount)
	require.Equal(t, "none", items[2].OAuthMatch)
	require.Nil(t, items[3].OAuthAccount)
	require.Equal(t, "ambiguous", items[3].OAuthMatch)
	require.Nil(t, items[4].OAuthAccount) // No provider-specific +tag rewriting.
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVaultOAuthMatchReadFailureIsNotNoMatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT id,name,platform,status,primary_email").WillReturnError(errors.New("offline"))
	require.Error(t, (&Handler{db: db}).matchVaultOAuthAccounts(context.Background(), []vaultRecord{{Email: "a@example.test", Platform: "claude"}}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVaultOAuthMatchPostgresMetadataAndDeletion(t *testing.T) {
	h, _ := toolsPostgresFixture(t)
	_, err := h.db.Exec(`UPDATE accounts SET extra='{"email_address":"Case@Example.test"}' WHERE id=1;
	UPDATE accounts SET credentials='{"email":"key@example.test","access_token":"TEST_ONLY_NOT_SELECTED"}',type='apikey' WHERE id=2;
	UPDATE accounts SET name='fallback@example.test' WHERE id=3;
	UPDATE accounts SET extra='{"email_address":"deleted@example.test"}',deleted_at=NOW() WHERE id=4;`)
	require.NoError(t, err)
	items := []vaultRecord{{Email: "case@example.test", Platform: "claude"}, {Email: "key@example.test", Platform: "claude"}, {Email: "fallback@example.test", Platform: "claude"}, {Email: "deleted@example.test", Platform: "claude"}}
	require.NoError(t, h.matchVaultOAuthAccounts(context.Background(), items))
	require.Equal(t, int64(1), items[0].OAuthAccount.ID)
	require.Nil(t, items[1].OAuthAccount)
	require.Equal(t, int64(3), items[2].OAuthAccount.ID)
	require.Nil(t, items[3].OAuthAccount)
	_, err = h.db.Exec(`UPDATE accounts SET extra='{"email_address":"other@example.test"}' WHERE id=1`)
	require.NoError(t, err)
	require.NoError(t, h.matchVaultOAuthAccounts(context.Background(), items))
	require.Nil(t, items[0].OAuthAccount)
}
