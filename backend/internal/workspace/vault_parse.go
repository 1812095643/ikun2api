package workspace

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"
)

type vaultSecret struct {
	Email             string `json:"email"`
	Password          string `json:"password"`
	AuxiliaryEmail    string `json:"auxiliary_email"`
	AuxiliaryPassword string `json:"auxiliary_password"`
	SessionKey        string `json:"session_key"`
	APIKey            string `json:"api_key"`
	MailURL           string `json:"mail_url"`
	AuxiliaryURL      string `json:"auxiliary_url"`
	Notes             string `json:"notes"`
}
type vaultEntry struct {
	vaultSecret
	Platform        string `json:"platform"`
	Title           string `json:"title"`
	Tags            string `json:"tags"`
	Status          string `json:"status"`
	AccountID       *int64 `json:"account_id"`
	ExistingID      string `json:"existing_id,omitempty"`
	ExpectedVersion int64  `json:"expected_version,omitempty"`
	Action          string `json:"action,omitempty"`
}
type vaultParseRow struct {
	Line  int        `json:"line"`
	Entry vaultEntry `json:"entry"`
	Error string     `json:"error,omitempty"`
}

func vaultEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	a, e := mail.ParseAddress(s)
	if e != nil || a.Address != s || len(s) > 320 {
		return "", fmt.Errorf("邮箱格式不正确")
	}
	at := strings.LastIndex(s, "@")
	return s[:at] + "@" + strings.ToLower(s[at+1:]), nil
}
func validateVaultEntry(s *vaultEntry) error {
	var err error
	s.Email, err = vaultEmail(s.Email)
	if err != nil {
		return err
	}
	if s.AuxiliaryEmail != "" {
		s.AuxiliaryEmail, err = vaultEmail(s.AuxiliaryEmail)
		if err != nil {
			return fmt.Errorf("辅助邮箱格式不正确")
		}
	}
	s.Platform = strings.ToLower(strings.TrimSpace(s.Platform))
	if s.Platform == "" {
		s.Platform = "claude"
	}
	if len(s.Platform) > 32 || strings.ContainsAny(s.Platform, " \r\n/") {
		return fmt.Errorf("平台格式不正确")
	}
	if s.Status == "" {
		s.Status = "unused"
	}
	switch s.Status {
	case "unused", "active", "archived":
	default:
		return fmt.Errorf("资料状态无效")
	}
	for _, v := range []string{s.Title, s.Tags} {
		if utf8.RuneCountInString(v) > 200 {
			return fmt.Errorf("名称或标签过长")
		}
	}
	for _, v := range []string{s.Password, s.AuxiliaryPassword, s.SessionKey, s.APIKey} {
		if len(v) > 8192 || strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("凭据过长或包含意外换行")
		}
	}
	if s.SessionKey != "" && !strings.HasPrefix(s.SessionKey, "sk-ant-sid") {
		return fmt.Errorf("SessionKey 类型不明确，请核对字段")
	}
	for _, v := range []string{s.MailURL, s.AuxiliaryURL} {
		if v != "" {
			u, e := url.Parse(v)
			if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || len(v) > 2048 {
				return fmt.Errorf("邮箱网址仅支持不带密码的 http(s) 地址")
			}
		}
	}
	if len(s.Notes) > 16000 {
		return fmt.Errorf("备注过长")
	}
	return nil
}
func vaultField(key string) string {
	k := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(strings.TrimPrefix(key, "\ufeff"))))
	switch k {
	case "邮箱账号", "主邮箱", "邮箱", "email", "emailaccount", "邮箱地址", "登录邮箱", "账号", "emailaddress":
		return "email"
	case "邮箱密码", "密码", "password", "mailpassword":
		return "password"
	case "辅助邮箱", "辅邮", "备用邮箱", "auxiliaryemail", "recoveryemail":
		return "auxiliary_email"
	case "辅邮密码", "辅助密码", "辅助邮箱密码", "备用邮箱密码", "auxiliarypassword", "recoverypassword":
		return "auxiliary_password"
	case "sessionkey", "sk":
		return "session_key"
	case "apikey", "api密钥":
		return "api_key"
	case "邮箱网址", "邮箱登录地址", "邮箱登录网址", "mailurl", "inboxurl":
		return "mail_url"
	case "辅邮地址", "辅助邮箱地址", "辅助邮箱网址", "auxiliaryurl":
		return "auxiliary_url"
	case "备注", "notes":
		return "notes"
	case "平台", "platform":
		return "platform"
	case "名称", "title":
		return "title"
	case "标签", "tags":
		return "tags"
	case "状态", "status":
		return "status"
	}
	return ""
}
func putVaultField(s *vaultEntry, f, v string) {
	switch f {
	case "email":
		s.Email = v
	case "password":
		s.Password = v
	case "auxiliary_email":
		s.AuxiliaryEmail = v
	case "auxiliary_password":
		s.AuxiliaryPassword = v
	case "session_key":
		s.SessionKey = v
	case "api_key":
		s.APIKey = v
	case "mail_url":
		s.MailURL = v
	case "auxiliary_url":
		s.AuxiliaryURL = v
	case "notes":
		s.Notes = v
	case "platform":
		s.Platform = v
	case "title":
		s.Title = v
	case "tags":
		s.Tags = v
	case "status":
		s.Status = v
	}
}

// Recognize only unambiguous column layouts. Password bytes are never trimmed,
// and unknown layouts must use a header or the explicit column mapping UI.
func vaultDelimitedEntry(values []string) (vaultEntry, string) {
	e := vaultEntry{}
	if len(values) == 0 {
		return e, "没有识别到资料"
	}
	e.Email = strings.TrimSpace(values[0])
	if _, err := vaultEmail(e.Email); err != nil {
		return e, "第一列应为主邮箱，请使用表头或自定义列顺序"
	}
	if len(values) < 2 {
		return e, "缺少邮箱密码列"
	}
	e.Password = values[1]
	key := func(value string) bool {
		if strings.HasPrefix(value, "sk-ant-sid") {
			e.SessionKey = value
			return true
		}
		if strings.HasPrefix(value, "sk-") {
			e.APIKey = value
			return true
		}
		return false
	}
	switch len(values) {
	case 2:
	case 3:
		if !key(values[2]) {
			return e, "第三列的 Key 类型不明确，请使用表头或自定义列顺序"
		}
	case 4:
		if _, err := vaultEmail(strings.TrimSpace(values[2])); err != nil {
			return e, "辅助邮箱列无法识别，请使用表头或自定义列顺序"
		}
		e.AuxiliaryEmail = strings.TrimSpace(values[2])
		e.AuxiliaryPassword = values[3]
	case 5:
		if values[2] == "" || key(values[2]) {
			e.AuxiliaryEmail = strings.TrimSpace(values[3])
			e.AuxiliaryPassword = values[4]
		} else if _, err := vaultEmail(strings.TrimSpace(values[2])); err == nil && key(values[4]) {
			e.AuxiliaryEmail = strings.TrimSpace(values[2])
			e.AuxiliaryPassword = values[3]
		} else {
			return e, "无法确定五列字段顺序，请使用表头或自定义列顺序"
		}
	default:
		return e, "列数无法识别，请使用表头或自定义列顺序，缺项保留空列"
	}
	return e, ""
}

func vaultLooksLikeTable(first string) bool {
	if strings.Contains(first, "----") {
		return false
	}
	separator := ","
	if strings.Contains(first, "\t") {
		separator = "\t"
	}
	if !strings.Contains(first, separator) {
		return false
	}
	firstCell := strings.Trim(strings.TrimSpace(strings.SplitN(first, separator, 2)[0]), "\"")
	_, err := vaultEmail(firstCell)
	return err == nil || vaultField(firstCell) != ""
}

func parseVaultImport(raw string) []vaultParseRow {
	rows := []vaultParseRow{}
	if len(raw) > 2<<20 {
		return []vaultParseRow{{Line: 1, Error: "资料超过 2 MiB，请分批导入"}}
	}
	text := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if text == "" {
		return rows
	}
	add := func(line int, e vaultEntry, problem string) {
		if problem == "" {
			if err := validateVaultEntry(&e); err != nil {
				problem = err.Error()
			}
		}
		rows = append(rows, vaultParseRow{Line: line, Entry: e, Error: problem})
	}
	if strings.HasPrefix(text, "[") || strings.HasPrefix(text, "{") {
		var entries []vaultEntry
		if strings.HasPrefix(text, "[") {
			if strictVaultJSON([]byte(text), &entries) != nil {
				return []vaultParseRow{{Line: 1, Error: "JSON 格式不正确"}}
			}
		} else {
			var obj struct {
				Items []vaultEntry `json:"items"`
			}
			if json.Unmarshal([]byte(text), &obj) != nil {
				return []vaultParseRow{{Line: 1, Error: "JSON 格式不正确"}}
			}
			if obj.Items != nil {
				if strictVaultJSON([]byte(text), &obj) != nil {
					return []vaultParseRow{{Line: 1, Error: "JSON 包含未知字段或多余内容"}}
				}
				entries = obj.Items
			} else {
				var e vaultEntry
				if strictVaultJSON([]byte(text), &e) != nil {
					return []vaultParseRow{{Line: 1, Error: "JSON 格式不正确"}}
				}
				entries = []vaultEntry{e}
			}
		}
		for i, e := range entries {
			add(i+1, e, "")
		}
	} else if first := strings.Split(text, "\n")[0]; vaultLooksLikeTable(first) {
		// Trim framing newlines only; trailing spaces may belong to a secret in
		// the final cell of a headerless paste.
		cr := csv.NewReader(strings.NewReader(strings.TrimLeft(strings.TrimPrefix(raw, "\ufeff"), "\r\n")))
		if strings.Contains(first, "\t") {
			cr.Comma = '\t'
		}
		cr.FieldsPerRecord = -1
		header, err := cr.Read()
		if err != nil {
			return []vaultParseRow{{Line: 1, Error: "表格表头不完整"}}
		}
		keys := []string{}
		seen := map[string]bool{}
		_, firstEmailErr := vaultEmail(strings.TrimSpace(header[0]))
		headerless := firstEmailErr == nil
		if headerless {
			e, problem := vaultDelimitedEntry(header)
			add(1, e, problem)
		}
		for _, k := range header {
			if headerless {
				break
			}
			k = vaultField(k)
			if k == "" || seen[k] {
				return []vaultParseRow{{Line: 1, Error: "表头存在未知或重复字段，请修改表头"}}
			}
			seen[k] = true
			keys = append(keys, k)
		}
		for n := 2; ; n++ {
			values, err := cr.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				add(n, vaultEntry{}, "表格引号或换行格式不完整")
				break
			}
			var e vaultEntry
			if headerless {
				entry, problem := vaultDelimitedEntry(values)
				add(n, entry, problem)
				continue
			}
			if len(values) != len(keys) {
				add(n, e, "列数与表头不一致")
				continue
			}
			for i, v := range values {
				putVaultField(&e, keys[i], v)
			}
			add(n, e, "")
		}
	} else {
		var current vaultEntry
		seen := map[string]bool{}
		start := 1
		problem := ""
		flush := func() {
			if len(seen) > 0 || problem != "" || current.Title != "" {
				add(start, current, problem)
			}
			current = vaultEntry{}
			seen = map[string]bool{}
			problem = ""
		}
		for n, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			delimiter := "----"
			if !strings.Contains(line, delimiter) && strings.Contains(line, "\t") {
				delimiter = "\t"
			}
			firstColumn := strings.SplitN(line, delimiter, 2)[0]
			_, firstEmailErr := vaultEmail(strings.TrimSpace(firstColumn))
			if strings.Contains(line, delimiter) && firstEmailErr == nil {
				flush()
				e, problem := vaultDelimitedEntry(strings.Split(line, delimiter))
				add(n+1, e, problem)
				continue
			}
			if strings.Trim(trimmed, "-—_= ") == "" {
				continue
			}
			at := strings.IndexAny(line, ":：")
			if at < 0 {
				if len(seen) > 0 {
					flush()
				}
				if current.Title != "" {
					problem = fmt.Sprintf("第 %d 行存在无法识别的内容，请整理后重试", n+1)
				}
				start = n + 1
				current.Title = trimmed
				continue
			}
			key := vaultField(line[:at])
			value := line[at:]
			_, size := utf8.DecodeRuneInString(value)
			value = strings.TrimPrefix(value[size:], " ")
			if key == "email" && (strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")) {
				key = "mail_url"
			}
			if key == "email" && seen["email"] {
				flush()
				start = n + 1
			}
			if key == "" {
				problem = fmt.Sprintf("第 %d 行字段无法识别，请修改或放入备注", n+1)
				continue
			}
			if seen[key] {
				problem = fmt.Sprintf("第 %d 行存在重复字段", n+1)
			}
			seen[key] = true
			putVaultField(&current, key, value)
		}
		flush()
	}
	if len(rows) > 500 {
		return []vaultParseRow{{Line: 1, Error: "每批最多 500 条资料"}}
	}
	return rows
}

func strictVaultJSON(raw []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}
