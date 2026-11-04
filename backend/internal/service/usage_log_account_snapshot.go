package service

import "strings"

// MaskAccountName keeps the original label recognizable without disclosing it in full.
// The hidden middle always uses exactly three asterisks.
func MaskAccountName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	mask := func(value string) string {
		runes := []rune(value)
		if len(runes) <= 2 {
			return "***"
		}
		prefix := max(1, min(3, len(runes)/3))
		suffix := max(1, min(2, (len(runes)-prefix-1)/2))
		return string(runes[:prefix]) + "***" + string(runes[len(runes)-suffix:])
	}
	if at := strings.LastIndex(name, "@"); at > 0 && at < len(name)-1 {
		return mask(name[:at]) + name[at:]
	}
	return mask(name)
}

// MaskAccountNameSnapshot returns a separate masked value; it never changes stored history.
func MaskAccountNameSnapshot(name *string) *string {
	if name == nil {
		return nil
	}
	masked := MaskAccountName(*name)
	if masked == "" {
		return nil
	}
	return &masked
}

// accountNameSnapshot 返回写入 usage_logs.account_name_snapshot 的账号名称快照。
// 快照在计费时刻冻结账号标识，历史用量记录不会因为账号改名、换绑或删除而漂移。
// 账号为空或名称空白时返回 nil，落库为 NULL。
func accountNameSnapshot(account *Account) *string {
	if account == nil {
		return nil
	}
	return optionalTrimmedStringPtr(account.Name)
}

// accountPlatformSnapshot 返回写入 usage_logs.account_platform_snapshot 的平台快照，
// 语义与 accountNameSnapshot 一致。
func accountPlatformSnapshot(account *Account) *string {
	if account == nil {
		return nil
	}
	return optionalTrimmedStringPtr(account.Platform)
}

// accountSnapshotLabel 把快照列还原成可展示的账号标签：优先用名称快照，
// 缺失时退回平台快照，都没有则返回空串，绝不回查 accounts 表。
func accountSnapshotLabel(name *string, platform *string) string {
	if name != nil {
		if trimmed := strings.TrimSpace(*name); trimmed != "" {
			return trimmed
		}
	}
	if platform != nil {
		return strings.TrimSpace(*platform)
	}
	return ""
}
