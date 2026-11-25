package notify

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MACOS-DO/sub4api/internal/monitoring/domain"
)

// ReadableSender adds transient labels to a copy immediately before transport.
// Lookup failures never block delivery and the labels are not persisted in SQLite.
type ReadableSender struct {
	Next Sender
	DB   *sql.DB
}

type identity struct {
	owner       int64
	name, email string
	found       bool
}

func cleanLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	r := []rune(strings.TrimSpace(s))
	if len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return string(r)
}
func (s ReadableSender) Send(ctx context.Context, d domain.Delivery, messageID, base string) (string, error) {
	ids := map[string]int64{}
	for _, part := range strings.Split(d.Payload.Scope, "/") {
		kind, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		if kind != "user" && kind != "key" && kind != "account" && kind != "group" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err == nil && id > 0 {
			ids[kind] = id
		}
	}
	if d.Payload.Sample != nil {
		ids = map[string]int64{"user": d.Payload.Sample.UserID, "key": d.Payload.Sample.KeyID, "account": d.Payload.Sample.AccountID, "group": d.Payload.Sample.GroupID}
	} else if ids["group"] == 0 && d.Payload.GroupID > 0 {
		ids["group"] = d.Payload.GroupID
	}
	if len(ids) == 0 {
		return s.Next.Send(ctx, d, messageID, base)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	lookup := func(kind string, id int64) identity {
		var x identity
		if s.DB == nil || id <= 0 {
			return x
		}
		if s.DB.QueryRowContext(lookupCtx, "SELECT owner_id, name, email FROM ops_monitor_v1_identity WHERE kind=$1 AND id=$2", kind, id).Scan(&x.owner, &x.name, &x.email) != nil {
			return identity{}
		}
		x.name = cleanLabel(x.name)
		x.email = cleanLabel(x.email)
		x.found = true
		return x
	}
	uid, kid := ids["user"], ids["key"]
	u := lookup("user", uid)
	k := lookup("key", kid)
	if uid == 0 && kid > 0 && k.found && k.owner > 0 {
		uid = k.owner
		u = lookup("user", uid)
	}
	parts := []string{}
	if uid > 0 {
		label := fmt.Sprintf("用户 #%d", uid)
		if u.found {
			if u.name != "" {
				label += " · " + u.name
			}
			if u.email != "" {
				label += " · " + u.email
			}
			if u.name == "" && u.email == "" {
				label += " · 名称未提供"
			}
		} else {
			label += " · 身份未确认"
		}
		parts = append(parts, label)
	}
	if kid > 0 {
		label := fmt.Sprintf("Key #%d", kid)
		if k.found && (uid == 0 || k.owner == uid) {
			if k.name != "" {
				label += " · " + k.name
			} else {
				label += " · 名称未提供"
			}
		} else {
			label += " · 名称或所属用户未确认"
		}
		parts = append(parts, label)
	}
	for _, item := range []struct{ key, label string }{{"account", "上游账号"}, {"group", "分组"}} {
		if id := ids[item.key]; id > 0 {
			x := lookup(item.key, id)
			label := fmt.Sprintf("%s #%d", item.label, id)
			if x.found && x.name != "" {
				label += " · " + x.name
			} else {
				label += " · 名称未确认"
			}
			parts = append(parts, label)
		}
	}
	if d.Payload.Sample != nil && d.Payload.Rule == "L02" {
		if uid == 0 {
			parts = append(parts, "用户 · 未采集")
		}
		if kid == 0 {
			parts = append(parts, "Key · 未采集")
		}
		if ids["account"] == 0 {
			parts = append(parts, "上游账号 · 当时尚未选出或未记录")
		}
		if ids["group"] == 0 {
			parts = append(parts, "分组 · 未采集")
		}
	}
	if len(parts) > 0 {
		prefix := ""
		if d.Payload.Sample != nil {
			prefix = "最近异常样本（不代表全部受影响请求）：\n"
		}
		d.Payload.DisplayScope = prefix + strings.Join(parts, "\n") + "\n范围：" + d.Payload.Scope
	}
	return s.Next.Send(ctx, d, messageID, base)
}
