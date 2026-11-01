-- Desktop access is an explicit opt-in beneath the existing Claude Code gate.
-- Existing groups retain CLI-only access after upgrading.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS allow_claude_desktop BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN groups.allow_claude_desktop IS
    'Allow Claude Desktop in Anthropic groups when claude_code_only is enabled';
