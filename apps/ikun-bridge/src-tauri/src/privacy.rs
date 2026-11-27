use anyhow::{ensure, Result};
use regex::Regex;
use serde_json::Value;
use std::sync::LazyLock;
static SYSTEM_LINE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)^\s*(?:x-anthropic-billing-header|x-claude-code-[\w-]+|x-claude-remote-[\w-]+|x-cc-[\w-]+|tengu_[\w-]+|cc_[\w-]+|cch|git[_ -]remote(?:[_ -]url)?|git[_ -]hash|git[_ -]commit|git[_ -]branch|(?:primary )?working directory|home directory|(?:repo|repository|project)[_ -](?:url|hash|id|root|path))\s*[:=]").unwrap()
});
const ALLOWED: &[&str] = &[
    "model",
    "messages",
    "max_tokens",
    "system",
    "stream",
    "stop_sequences",
    "temperature",
    "top_p",
    "top_k",
    "tools",
    "tool_choice",
    "thinking",
    "cache_control",
    "container",
    "context_management",
    "output_config",
    "output_format",
    "service_tier",
    "speed",
    "inference_geo",
    "compaction",
    "thread",
    "fallbacks",
    "fallback_credit_token",
    "anthropic_version",
    "anthropic_beta",
];
fn environment(text: &str) -> String {
    let lines: Vec<_> = text.split_inclusive('\n').collect();
    let Some(first) = lines.iter().position(|l| !l.trim().is_empty()) else {
        return text.into();
    };
    let start = match lines[first].trim() {
        "# Environment"
            if lines.get(first + 1).is_some_and(|l| {
                l.trim() == "You have been invoked in the following environment:"
            }) =>
        {
            first + 2
        }
        "# Environment update" => first + 1,
        _ => return text.into(),
    };
    let mut out = String::new();
    let mut active = true;
    let mut skipped = None;
    for (i, line) in lines.iter().enumerate() {
        if i < start || !active {
            out.push_str(line);
            continue;
        }
        let field = line.trim_start_matches([' ', '\t']);
        if !field.starts_with("- ") {
            active = false;
            out.push_str(line);
            continue;
        }
        let indent = line.len() - field.len();
        if skipped.is_some_and(|s| indent > s) {
            continue;
        }
        skipped = None;
        let field = field[2..].trim();
        if [
            "Additional working directories:",
            "Additional working directories added:",
            "Additional working directories removed:",
        ]
        .iter()
        .any(|p| field.starts_with(p))
        {
            skipped = Some(indent);
            continue;
        }
        if ["Primary working directory:", "Scratchpad directory:"]
            .iter()
            .any(|p| field.starts_with(p))
        {
            continue;
        }
        const PREFIX: &str = "The scratchpad directory announced earlier (";
        const SUFFIX:&str=") is out of use: put no new files there. Files that you wrote there may still be at that path. To go on working with one, first copy it under the primary working directory: a tool that attaches or publishes a file may refuse the old path.";
        if field.starts_with(PREFIX) && field.ends_with(SUFFIX) {
            out.push_str(&line[..indent]);
            out.push_str("- The scratchpad directory announced earlier");
            out.push_str(&SUFFIX[1..]);
            if line.ends_with("\r\n") {
                out.push_str("\r\n")
            } else if line.ends_with('\n') {
                out.push('\n')
            }
            continue;
        }
        out.push_str(line)
    }
    out
}
pub fn auto_context(text: &str) -> String {
    let t = text.trim();
    if !(t.starts_with("<system-reminder>\n") || t.starts_with("<system-reminder>\r\n")) {
        return environment(text);
    }
    if !t.ends_with("</system-reminder>") || t.matches("<system-reminder>").count() != 1 {
        return text.into();
    }
    let inner = t
        .strip_prefix("<system-reminder>")
        .unwrap()
        .strip_suffix("</system-reminder>")
        .unwrap()
        .trim()
        .replace("\r\n", "\n");
    let clean = if inner
        .starts_with("# Environment\nYou have been invoked in the following environment:")
        || inner.starts_with("# Environment update\n")
    {
        environment(&inner)
    } else {
        let announced = inner.starts_with(
            "As you answer the user's questions, you can use the following context:\n",
        ) || inner.starts_with(
            "The session context has changed; these values replace the earlier ones:\n",
        ) || (inner.starts_with("The session context was re-read ")
            && inner.contains("; these values replace the earlier ones:\n"));
        if !announced||!inner.contains("Claude Code attached this context automatically; it isn't part of the user's message."){return text.into()}
        let mut out = String::new();
        let mut skip = false;
        let mut kept = false;
        let mut changed = false;
        for l in inner.split_inclusive('\n') {
            if let Some(section) = l.strip_prefix("# ") {
                skip = !matches!(section.trim(), "currentDate" | "perforceMode");
                changed |= skip;
                kept |= !skip;
            }
            if l.trim_start()
                .starts_with("Claude Code attached this context automatically;")
            {
                skip = false
            }
            if !skip {
                out.push_str(l)
            }
        }
        if !changed {
            return text.into();
        }
        if !kept {
            return String::new();
        }
        out
    };
    if clean == inner {
        return text.into();
    }
    format!("<system-reminder>\n{}\n</system-reminder>", clean.trim())
}
fn system_text(s: &str) -> String {
    let text = auto_context(s);
    if text
        .trim_start()
        .starts_with("gitStatus: This is the git status at the start of the conversation.")
    {
        return String::new();
    }
    text.split_inclusive('\n')
        .filter(|l| !SYSTEM_LINE.is_match(l))
        .collect()
}
fn cache(v: &mut Value) -> Result<()> {
    if let Some(o) = v.as_object_mut() {
        if let Some(c) = o.get_mut("cache_control") {
            if c.is_null() {
                o.remove("cache_control");
            } else {
                let c = c
                    .as_object_mut()
                    .ok_or_else(|| anyhow::anyhow!("cache_control 格式错误"))?;
                ensure!(
                    c.get("type").and_then(Value::as_str) == Some("ephemeral"),
                    "cache_control.type 无效"
                );
                c.retain(|k, _| matches!(k.as_str(), "type" | "ttl" | "scope"));
            }
        }
    }
    Ok(())
}
fn tool(v: &mut Value) -> Result<()> {
    if let Some(o) = v.as_object_mut() {
        // Attribution is removed together with the negotiated beta; do not touch input_schema.
        o.remove("ownership_metadata");
    }
    cache(v)
}
pub fn sanitize(raw: &[u8]) -> Result<Vec<u8>> {
    let mut v = crate::config_edit::strict_value(raw)?;
    let original = v.clone();
    let o = v
        .as_object_mut()
        .ok_or_else(|| anyhow::anyhow!("模型请求必须是 JSON 对象"))?;
    ensure!(
        !o.contains_key("safeguards"),
        "此请求依赖服务端工具权限检查，请改用客户端逐次授权模式"
    );
    ensure!(
        o.get("mcp_servers")
            .is_none_or(|v| v.is_null() || v.as_array().is_some_and(Vec::is_empty)),
        "此请求需要服务端 MCP 连接，当前只支持本地工具"
    );
    ensure!(
        o.get("model")
            .and_then(Value::as_str)
            .is_some_and(|s| !s.is_empty()),
        "缺少模型名称"
    );
    ensure!(
        o.get("messages").is_some_and(Value::is_array),
        "消息列表格式无效"
    );
    o.retain(|k, _| ALLOWED.contains(&k.as_str()));
    if let Some(s) = o.get_mut("system") {
        if let Some(text) = s.as_str() {
            *s = Value::String(system_text(text));
        } else if let Some(blocks) = s.as_array_mut() {
            for b in blocks.iter_mut() {
                if let Some(m) = b.as_object_mut() {
                    m.retain(|k, _| {
                        matches!(k.as_str(), "type" | "text" | "cache_control" | "citations")
                    });
                    if let Some(Value::String(t)) = m.get_mut("text") {
                        *t = system_text(t)
                    }
                }
                cache(b)?;
            }
            blocks.retain(|b| {
                !b.get("text")
                    .and_then(Value::as_str)
                    .is_some_and(|s| s.trim().is_empty())
            });
        }
    }
    if let Some(tools) = o.get_mut("tools").and_then(Value::as_array_mut) {
        for t in tools {
            tool(t)?
        }
    }
    if let Some(messages) = o.get_mut("messages").and_then(Value::as_array_mut) {
        for m in messages {
            if let Some(text) = m.get("content").and_then(Value::as_str) {
                m["content"] = Value::String(auto_context(text));
            } else if let Some(blocks) = m.get_mut("content").and_then(Value::as_array_mut) {
                for b in blocks.iter_mut() {
                    if b.get("type").and_then(Value::as_str) == Some("text") {
                        if let Some(Value::String(t)) = b.get_mut("text") {
                            *t = auto_context(t)
                        }
                    }
                    if b.get("type").and_then(Value::as_str) == Some("tool_addition") {
                        if let Some(t) = b.get_mut("tool").and_then(|x| x.get_mut("definition")) {
                            tool(t)?
                        }
                    }
                    cache(b)?;
                }
                blocks.retain(|b| {
                    !(b.get("type").and_then(Value::as_str) == Some("text")
                        && b.get("text")
                            .and_then(Value::as_str)
                            .is_some_and(|s| s.trim().is_empty()))
                });
            }
        }
    }
    if let Some(Value::String(beta)) = o.get_mut("anthropic_beta") {
        *beta = clean_beta(beta)
    }
    cache(&mut v)?;
    if v == original {
        Ok(raw.to_vec())
    } else {
        Ok(serde_json::to_vec(&v)?)
    }
}
pub fn clean_beta(s: &str) -> String {
    s.split(',')
        .map(str::trim)
        .filter(|s| !s.is_empty() && *s != "tool-provenance-2026-10-26")
        .collect::<Vec<_>>()
        .join(",")
}
pub fn allowed_header(name: &str) -> bool {
    matches!(
        name,
        "anthropic-version" | "anthropic-beta" | "accept" | "user-agent" | "x-app"
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_safety_and_remote_tool_requests_instead_of_stripping_them() {
        for raw in [
            br#"{"model":"claude-test","messages":[],"safeguards":{"enabled":true}}"#.as_slice(),
            br#"{"model":"claude-test","messages":[],"mcp_servers":[{"name":"remote"}]}"#,
        ] {
            assert!(sanitize(raw).is_err());
        }
        let raw = br#"{ "model": "claude-test", "messages": [] }"#;
        assert_eq!(sanitize(raw).unwrap(), raw);
    }
    #[test]
    fn preserves_user_tool_and_signatures() {
        let raw=br#"{"model":"claude-test","messages":[{"role":"user","content":[{"type":"tool_result","content":"Primary working directory: user text","tool_use_id":"x"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"safe","signature":"abc"}]}],"tools":[{"name":"Read","input_schema":{"properties":{"ownership_metadata":{"type":"string"}}},"ownership_metadata":{"server_url":"https://secret.invalid"}}],"system":[{"type":"text","text":"You are helpful.\nx-anthropic-billing-header: cc_gateway=1;\n"}],"metadata":{"private":"x"},"future_private":"x"}"#;
        let v: Value = serde_json::from_slice(&sanitize(raw).unwrap()).unwrap();
        assert!(v.get("metadata").is_none());
        assert!(v.get("future_private").is_none());
        assert_eq!(v["system"][0]["text"], "You are helpful.\n");
        assert_eq!(v["messages"][1]["content"][0]["signature"], "abc");
        assert!(v["tools"][0].get("ownership_metadata").is_none());
        assert!(v["tools"][0]["input_schema"]["properties"]
            .get("ownership_metadata")
            .is_some());
    }
    #[test]
    fn automatic_template_only() {
        let t="# Environment\nYou have been invoked in the following environment:\n- Primary working directory: C:/PRIVATE\n- Platform: win32\n\nUser: Primary working directory: keep\n";
        let s = auto_context(t);
        assert!(!s.contains("PRIVATE"));
        assert!(s.contains("User: Primary working directory: keep"));
        assert_eq!(
            auto_context("Working directory: user asked this"),
            "Working directory: user asked this"
        );
    }
    #[test]
    fn duplicate_rejected() {
        assert!(sanitize(br#"{"model":"a","model":"b"}"#).is_err());
    }
}
