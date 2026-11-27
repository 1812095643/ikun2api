//! Lossless field edits. No read/deserialize/reserialize of an existing file.
use anyhow::{bail, ensure, Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{
    collections::HashSet,
    path::{Path, PathBuf},
};

#[derive(Clone, Debug)]
struct Member {
    key: String,
    start: usize,
    node: Node,
}
#[derive(Clone, Debug)]
enum Kind {
    Object(Vec<Member>),
    Array(Vec<Node>),
    Scalar(Value),
}
#[derive(Clone, Debug)]
struct Node {
    start: usize,
    end: usize,
    kind: Kind,
}
impl Node {
    fn value(&self) -> Value {
        match &self.kind {
            Kind::Object(m) => {
                Value::Object(m.iter().map(|m| (m.key.clone(), m.node.value())).collect())
            }
            Kind::Array(a) => Value::Array(a.iter().map(Node::value).collect()),
            Kind::Scalar(v) => v.clone(),
        }
    }
    fn object(&self) -> Result<&Vec<Member>> {
        match &self.kind {
            Kind::Object(m) => Ok(m),
            _ => bail!("目标配置项必须是 JSON 对象"),
        }
    }
    fn child(&self, key: &str) -> Result<Option<&Node>> {
        Ok(self
            .object()?
            .iter()
            .find(|m| m.key == key)
            .map(|m| &m.node))
    }
}
struct Parser<'a> {
    text: &'a str,
    at: usize,
    comments: bool,
}
impl<'a> Parser<'a> {
    fn skip(&mut self) -> Result<()> {
        loop {
            while self
                .text
                .as_bytes()
                .get(self.at)
                .is_some_and(u8::is_ascii_whitespace)
            {
                self.at += 1;
            }
            if self.comments && self.text[self.at..].starts_with("//") {
                self.at += self.text[self.at..]
                    .find('\n')
                    .unwrap_or(self.text.len() - self.at);
            } else if self.comments && self.text[self.at..].starts_with("/*") {
                self.at += self.text[self.at + 2..]
                    .find("*/")
                    .context("JSON 注释未闭合")?
                    + 4;
            } else {
                return Ok(());
            }
        }
    }
    fn string(&mut self) -> Result<String> {
        let start = self.at;
        ensure!(
            self.text.as_bytes().get(self.at) == Some(&b'"'),
            "JSON 对象键必须是字符串"
        );
        self.at += 1;
        while let Some(&b) = self.text.as_bytes().get(self.at) {
            self.at += 1;
            if b == b'\\' {
                self.at += 1;
            } else if b == b'"' {
                return Ok(serde_json::from_str(&self.text[start..self.at])?);
            }
        }
        bail!("JSON 字符串未闭合")
    }
    fn node(&mut self, depth: usize) -> Result<Node> {
        ensure!(depth <= 96, "JSON 嵌套过深");
        self.skip()?;
        let start = self.at;
        let kind = match self.text.as_bytes().get(self.at) {
            Some(b'{') => {
                self.at += 1;
                let mut items = Vec::new();
                let mut seen = HashSet::new();
                self.skip()?;
                while self.text.as_bytes().get(self.at) != Some(&b'}') {
                    let ms = self.at;
                    let key = self.string()?;
                    ensure!(seen.insert(key.clone()), "JSON 存在重复键：{key}");
                    self.skip()?;
                    ensure!(
                        self.text.as_bytes().get(self.at) == Some(&b':'),
                        "JSON 缺少冒号"
                    );
                    self.at += 1;
                    let node = self.node(depth + 1)?;
                    items.push(Member {
                        key,
                        start: ms,
                        node,
                    });
                    self.skip()?;
                    if self.text.as_bytes().get(self.at) != Some(&b',') {
                        break;
                    }
                    self.at += 1;
                    self.skip()?;
                    ensure!(
                        self.comments || self.text.as_bytes().get(self.at) != Some(&b'}'),
                        "JSON 不允许末尾逗号"
                    );
                }
                ensure!(
                    self.text.as_bytes().get(self.at) == Some(&b'}'),
                    "JSON 对象未闭合"
                );
                self.at += 1;
                Kind::Object(items)
            }
            Some(b'[') => {
                self.at += 1;
                let mut items = Vec::new();
                self.skip()?;
                while self.text.as_bytes().get(self.at) != Some(&b']') {
                    items.push(self.node(depth + 1)?);
                    self.skip()?;
                    if self.text.as_bytes().get(self.at) != Some(&b',') {
                        break;
                    }
                    self.at += 1;
                    self.skip()?;
                    ensure!(
                        self.comments || self.text.as_bytes().get(self.at) != Some(&b']'),
                        "JSON 不允许末尾逗号"
                    );
                }
                ensure!(
                    self.text.as_bytes().get(self.at) == Some(&b']'),
                    "JSON 数组未闭合"
                );
                self.at += 1;
                Kind::Array(items)
            }
            Some(b'"') => Kind::Scalar(Value::String(self.string()?)),
            Some(_) => {
                while self
                    .text
                    .as_bytes()
                    .get(self.at)
                    .is_some_and(|b| !b.is_ascii_whitespace() && !b",]}:/".contains(b))
                {
                    self.at += 1;
                }
                ensure!(self.at > start, "无法解析 JSON 字段");
                Kind::Scalar(serde_json::from_str(&self.text[start..self.at])?)
            }
            None => bail!("JSON 不完整"),
        };
        Ok(Node {
            start,
            end: self.at,
            kind,
        })
    }
}
fn parse(text: &str, comments: bool) -> Result<Node> {
    let mut p = Parser {
        text,
        at: 0,
        comments,
    };
    let root = p.node(0)?;
    p.skip()?;
    ensure!(p.at == text.len(), "JSON 末尾存在额外内容");
    Ok(root)
}
pub fn strict_value(bytes: &[u8]) -> Result<Value> {
    Ok(parse(std::str::from_utf8(bytes)?, false)?.value())
}

#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum Target {
    Field { path: Vec<String> },
    Entry { path: Vec<String>, id: String },
}
impl Target {
    pub fn field(path: &[&str]) -> Self {
        Self::Field {
            path: path.iter().map(|s| s.to_string()).collect(),
        }
    }
    fn path(&self) -> &[String] {
        match self {
            Self::Field { path } | Self::Entry { path, .. } => path,
        }
    }
}
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Edit {
    pub target: Target,
    pub value: Option<Value>,
}
impl Edit {
    pub fn set(path: &[&str], value: impl Into<Value>) -> Self {
        Self {
            target: Target::field(path),
            value: Some(value.into()),
        }
    }
}
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Change {
    pub target: Target,
    pub before: Option<Value>,
    pub before_raw: Option<String>,
    pub after: Option<Value>,
    pub parents: Vec<Vec<String>>,
}
#[derive(Clone, Debug)]
enum Encoding {
    Utf8(bool),
    Utf16(bool),
}
#[derive(Clone, Debug)]
pub struct Document {
    text: String,
    encoding: Encoding,
    comments: bool,
}
impl Document {
    pub fn decode(raw: &[u8], comments: bool) -> Result<Self> {
        ensure!(
            raw.len() <= 8 * 1024 * 1024,
            "配置文件超过 8MB，停止自动修改"
        );
        let (text, encoding) = if raw.starts_with(&[0xff, 0xfe]) || raw.starts_with(&[0xfe, 0xff]) {
            let le = raw[0] == 0xff;
            ensure!(raw.len() % 2 == 0, "UTF-16 文件不完整");
            let chars: Vec<u16> = raw[2..]
                .chunks_exact(2)
                .map(|b| {
                    if le {
                        u16::from_le_bytes([b[0], b[1]])
                    } else {
                        u16::from_be_bytes([b[0], b[1]])
                    }
                })
                .collect();
            (String::from_utf16(&chars)?, Encoding::Utf16(le))
        } else {
            let bom = raw.starts_with(&[0xef, 0xbb, 0xbf]);
            (
                String::from_utf8(raw[if bom { 3 } else { 0 }..].to_vec())?,
                Encoding::Utf8(bom),
            )
        };
        parse(&text, comments)?.object()?;
        Ok(Self {
            text,
            encoding,
            comments,
        })
    }
    pub fn empty(comments: bool) -> Self {
        Self {
            text: "{}\n".into(),
            encoding: Encoding::Utf8(false),
            comments,
        }
    }
    pub fn encode(&self) -> Vec<u8> {
        match self.encoding {
            Encoding::Utf8(bom) => {
                let mut b = if bom { vec![0xef, 0xbb, 0xbf] } else { vec![] };
                b.extend(self.text.as_bytes());
                b
            }
            Encoding::Utf16(le) => {
                let mut b = if le {
                    vec![0xff, 0xfe]
                } else {
                    vec![0xfe, 0xff]
                };
                for u in self.text.encode_utf16() {
                    b.extend(if le { u.to_le_bytes() } else { u.to_be_bytes() });
                }
                b
            }
        }
    }
    pub fn value(&self) -> Result<Value> {
        Ok(parse(&self.text, self.comments)?.value())
    }
    fn locate<'a>(&self, root: &'a Node, path: &[String]) -> Result<Option<&'a Node>> {
        let mut n = root;
        for key in path {
            match n.child(key)? {
                Some(v) => n = v,
                None => return Ok(None),
            }
        }
        Ok(Some(n))
    }
    fn target_node<'a>(&self, root: &'a Node, t: &Target) -> Result<Option<&'a Node>> {
        let n = self.locate(root, t.path())?;
        match t {
            Target::Field { .. } => Ok(n),
            Target::Entry { id, .. } => {
                let Some(n) = n else { return Ok(None) };
                let Kind::Array(a) = &n.kind else {
                    bail!("配置 entries 必须是数组")
                };
                let matches: Vec<_> = a
                    .iter()
                    .filter(|n| n.value().get("id").and_then(Value::as_str) == Some(id))
                    .collect();
                ensure!(matches.len() <= 1, "配置库存在重复 ID");
                Ok(matches.first().copied())
            }
        }
    }
    pub fn current(&self, t: &Target) -> Result<Option<Value>> {
        let root = parse(&self.text, self.comments)?;
        Ok(self.target_node(&root, t)?.map(Node::value))
    }
    pub fn apply(&mut self, edit: &Edit) -> Result<Change> {
        ensure!(!edit.target.path().is_empty(), "不能替换配置文件根对象");
        let root = parse(&self.text, self.comments)?;
        let before = self.target_node(&root, &edit.target)?;
        let mut change = Change {
            target: edit.target.clone(),
            before: before.map(Node::value),
            before_raw: before.map(|n| self.text[n.start..n.end].to_owned()),
            after: edit.value.clone(),
            parents: vec![],
        };
        if change.before == change.after {
            return Ok(change);
        }
        let old = self.text.clone();
        let outcome =
            self.apply_inner(&edit.target, edit.value.as_ref(), None, &mut change.parents);
        if let Err(e) = outcome {
            self.text = old;
            return Err(e);
        }
        if let Err(e) = parse(&self.text, self.comments) {
            self.text = old;
            return Err(e);
        }
        ensure!(
            self.current(&edit.target)? == edit.value,
            "字段写入校验失败"
        );
        Ok(change)
    }
    fn apply_inner(
        &mut self,
        t: &Target,
        value: Option<&Value>,
        raw: Option<&str>,
        parents: &mut Vec<Vec<String>>,
    ) -> Result<()> {
        let root = parse(&self.text, self.comments)?;
        if let Some(n) = self.target_node(&root, t)? {
            if let Some(v) = value {
                self.text.replace_range(
                    n.start..n.end,
                    &raw.map(str::to_owned).unwrap_or(serde_json::to_string(v)?),
                );
                return Ok(());
            }
            return self.remove_existing(&root, t);
        }
        let Some(v) = value else { return Ok(()) };
        let path = t.path();
        for depth in 1..path.len() {
            let root = parse(&self.text, self.comments)?;
            if self.locate(&root, &path[..depth])?.is_none() {
                let parent = Target::Field {
                    path: path[..depth].to_vec(),
                };
                self.apply_inner(&parent, Some(&serde_json::json!({})), None, &mut vec![])?;
                parents.push(path[..depth].to_vec());
            }
        }
        if let Target::Entry { path, .. } = t {
            let root = parse(&self.text, self.comments)?;
            if self.locate(&root, path)?.is_none() {
                self.apply_inner(
                    &Target::Field { path: path.clone() },
                    Some(&serde_json::json!([])),
                    None,
                    &mut vec![],
                )?;
                parents.push(path.clone());
            }
            let root = parse(&self.text, self.comments)?;
            let array = self.locate(&root, path)?.context("缺少 entries")?;
            let Kind::Array(a) = &array.kind else {
                bail!("配置 entries 必须是数组")
            };
            return self.insert(
                array,
                a.last().map(|n| n.end),
                raw.map(str::to_owned).unwrap_or(serde_json::to_string(v)?),
            );
        }
        let root = parse(&self.text, self.comments)?;
        let obj = self
            .locate(&root, &path[..path.len() - 1])?
            .context("目标父项不存在")?;
        let members = obj.object()?;
        let content = format!(
            "{}: {}",
            serde_json::to_string(path.last().unwrap())?,
            raw.map(str::to_owned).unwrap_or(serde_json::to_string(v)?)
        );
        self.insert(obj, members.last().map(|m| m.node.end), content)
    }
    fn insert(&mut self, node: &Node, last: Option<usize>, content: String) -> Result<()> {
        let newline = if self.text.contains("\r\n") {
            "\r\n"
        } else {
            "\n"
        };
        let line_start = self.text[..node.start].rfind('\n').map_or(0, |p| p + 1);
        let indent: String = self.text[line_start..node.start]
            .chars()
            .take_while(|c| *c == ' ' || *c == '\t')
            .collect();
        let unit = self
            .text
            .lines()
            .find_map(|l| {
                let s: String = l.chars().take_while(|c| *c == ' ' || *c == '\t').collect();
                if s.is_empty() {
                    None
                } else {
                    Some(s)
                }
            })
            .unwrap_or("  ".into());
        if let Some(end) = last {
            let between = &self.text[end..node.end - 1];
            let mut p = Parser {
                text: between,
                at: 0,
                comments: self.comments,
            };
            p.skip()?;
            let comma = between.as_bytes().get(p.at) == Some(&b',');
            let point = if comma { end + p.at + 1 } else { end };
            let fragment = format!(
                "{}{newline}{indent}{unit}{content}{}",
                if comma { "" } else { "," },
                if comma { "," } else { "" }
            );
            self.text.insert_str(point, &fragment);
        } else {
            self.text.insert_str(
                node.end - 1,
                &format!("{newline}{indent}{unit}{content}{newline}{indent}"),
            );
        }
        Ok(())
    }
    fn remove_existing(&mut self, root: &Node, t: &Target) -> Result<()> {
        let path = t.path();
        let (container, spans, index) = match t {
            Target::Field { .. } => {
                let n = self
                    .locate(root, &path[..path.len() - 1])?
                    .context("缺少父对象")?;
                let m = n.object()?;
                let i = m
                    .iter()
                    .position(|m| Some(&m.key) == path.last())
                    .context("字段不存在")?;
                (
                    n,
                    m.iter().map(|m| (m.start, m.node.end)).collect::<Vec<_>>(),
                    i,
                )
            }
            Target::Entry { id, .. } => {
                let n = self.locate(root, path)?.context("缺少数组")?;
                let Kind::Array(a) = &n.kind else {
                    bail!("不是数组")
                };
                let i = a
                    .iter()
                    .position(|n| n.value().get("id").and_then(Value::as_str) == Some(id))
                    .context("条目不存在")?;
                (n, a.iter().map(|n| (n.start, n.end)).collect::<Vec<_>>(), i)
            }
        };
        let (start, end) = spans[index];
        // Remove only the value/member and its separator. Preserve comments outside it.
        let tail = &self.text[end..container.end - 1];
        let mut p = Parser {
            text: tail,
            at: 0,
            comments: self.comments,
        };
        p.skip()?;
        if tail.as_bytes().get(p.at) == Some(&b',') {
            self.text.replace_range(end + p.at..end + p.at + 1, "");
            self.text.replace_range(start..end, "");
        } else if index > 0 {
            let prev = spans[index - 1].1;
            let between = &self.text[prev..start];
            let mut p = Parser {
                text: between,
                at: 0,
                comments: self.comments,
            };
            p.skip()?;
            ensure!(
                between.as_bytes().get(p.at) == Some(&b','),
                "找不到配置分隔符"
            );
            let comma = prev + p.at;
            self.text.replace_range(start..end, "");
            self.text.replace_range(comma..comma + 1, "");
        } else {
            self.text.replace_range(start..end, "");
        }
        Ok(())
    }
    pub fn undo(&mut self, c: &Change) -> Result<bool> {
        if self.current(&c.target)? == c.before {
            return Ok(true);
        }
        if self.current(&c.target)? != c.after {
            return Ok(false);
        }
        self.apply_inner(
            &c.target,
            c.before.as_ref(),
            c.before_raw.as_deref(),
            &mut vec![],
        )?;
        for p in c.parents.iter().rev() {
            let t = Target::Field { path: p.clone() };
            if self.current(&t)?.is_some_and(|v| {
                matches!(v,Value::Object(ref x) if x.is_empty())
                    || matches!(v,Value::Array(ref x) if x.is_empty())
            }) {
                self.apply_inner(&t, None, None, &mut vec![])?;
            }
        }
        parse(&self.text, self.comments)?;
        Ok(true)
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FileRecord {
    pub path: PathBuf,
    pub existed: bool,
    pub original: Vec<u8>,
    pub applied: Vec<u8>,
    pub changes: Vec<Change>,
    pub comments: bool,
}
impl FileRecord {
    pub fn prepare(path: &Path, edits: &[Edit], comments: bool) -> Result<Self> {
        ensure!(
            !path
                .symlink_metadata()
                .is_ok_and(|m| m.file_type().is_symlink()),
            "不自动改写符号链接：{}",
            path.display()
        );
        let existed = path.exists();
        let original = if existed {
            std::fs::read(path)?
        } else {
            vec![]
        };
        let mut doc = if existed {
            Document::decode(&original, comments)?
        } else {
            Document::empty(comments)
        };
        let mut changes = vec![];
        for e in edits {
            let c = doc.apply(e)?;
            if c.before != c.after {
                changes.push(c)
            }
        }
        Ok(Self {
            path: path.to_owned(),
            existed,
            original,
            applied: doc.encode(),
            changes,
            comments,
        })
    }
    pub fn write(&self) -> Result<()> {
        compare_write(
            &self.path,
            if self.existed {
                Some(&self.original)
            } else {
                None
            },
            Some(&self.applied),
        )
    }
    pub fn rollback(&self) -> Result<Vec<String>> {
        let raw = match std::fs::read(&self.path) {
            Ok(b) => b,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(vec![]),
            Err(e) => return Err(e.into()),
        };
        if raw == self.original && self.existed {
            return Ok(vec![]);
        }
        if raw == self.applied {
            if self.existed {
                compare_write(&self.path, Some(&raw), Some(&self.original))?
            } else {
                compare_write(&self.path, Some(&raw), None)?
            }
            return Ok(vec![]);
        }
        let mut doc = Document::decode(&raw, self.comments)?;
        let mut conflicts = vec![];
        for c in self.changes.iter().rev() {
            if !doc.undo(c)? {
                conflicts.push(format!(
                    "{}: {}",
                    self.path.display(),
                    c.target.path().join(".")
                ));
            }
        }
        // Guard against an intervening editor even during undo.
        compare_write(&self.path, Some(&raw), Some(&doc.encode()))?;
        Ok(conflicts)
    }
}
pub fn compare_write(
    path: &Path,
    expected: Option<&[u8]>,
    replacement: Option<&[u8]>,
) -> Result<()> {
    ensure!(
        !path
            .symlink_metadata()
            .is_ok_and(|m| m.file_type().is_symlink()),
        "配置路径已变为符号链接，停止写入"
    );
    let mut options = std::fs::OpenOptions::new();
    options.read(true);
    // Deny concurrent writes while allowing atomic replacement of the name. A
    // writer already holding the original open makes this operation fail safely.
    #[cfg(windows)]
    {
        use std::os::windows::fs::OpenOptionsExt;
        options.share_mode(0x1 | 0x4);
    }
    let original = match options.open(path) {
        Ok(f) => Some(f),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => None,
        Err(e) => return Err(e.into()),
    };
    let current = original
        .as_ref()
        .map(|f| {
            use std::io::Read;
            let mut b = vec![];
            let mut f = f;
            f.read_to_end(&mut b).map(|_| b)
        })
        .transpose()?;
    ensure!(
        current.as_deref() == expected,
        "配置已被其他程序修改，请重新检查：{}",
        path.display()
    );
    match replacement {
        Some(bytes) => atomic_write_impl(path, bytes, expected.is_some()),
        None => {
            if original.is_some() {
                std::fs::remove_file(path)?
            }
            Ok(())
        }
    }
}
pub fn atomic_write(path: &Path, bytes: &[u8]) -> Result<()> {
    atomic_write_impl(path, bytes, true)
}
fn atomic_write_impl(path: &Path, bytes: &[u8], replace: bool) -> Result<()> {
    let parent = path.parent().context("缺少配置目录")?;
    std::fs::create_dir_all(parent)?;
    let mut temp = tempfile::NamedTempFile::new_in(parent)?;
    use std::io::Write;
    temp.write_all(bytes)?;
    temp.as_file().sync_all()?;
    if let Ok(meta) = path.metadata() {
        temp.as_file().set_permissions(meta.permissions())?;
    }
    let temp = temp.into_temp_path();
    #[cfg(windows)]
    {
        use std::os::windows::ffi::OsStrExt;
        use windows_sys::Win32::Storage::FileSystem::{
            MoveFileExW, ReplaceFileW, MOVEFILE_REPLACE_EXISTING, MOVEFILE_WRITE_THROUGH,
        };
        let a: Vec<u16> = temp.as_os_str().encode_wide().chain(Some(0)).collect();
        let b: Vec<u16> = path.as_os_str().encode_wide().chain(Some(0)).collect();
        let ok = unsafe {
            if replace && path.exists() {
                ReplaceFileW(
                    b.as_ptr(),
                    a.as_ptr(),
                    std::ptr::null(),
                    0,
                    std::ptr::null(),
                    std::ptr::null(),
                )
            } else {
                MoveFileExW(
                    a.as_ptr(),
                    b.as_ptr(),
                    (if replace {
                        MOVEFILE_REPLACE_EXISTING
                    } else {
                        0
                    }) | MOVEFILE_WRITE_THROUGH,
                )
            }
        };
        ensure!(ok != 0, "原子替换失败：{}", std::io::Error::last_os_error());
    }
    #[cfg(not(windows))]
    {
        if replace {
            temp.persist(path).map_err(|e| anyhow::anyhow!(e.error))?;
        } else {
            temp.persist_noclobber(path)
                .map_err(|e| anyhow::anyhow!(e.error))?;
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn preserve_comments_and_unrelated_bytes() {
        let raw=b"\xef\xbb\xbf{\r\n  // keep\r\n  \"mcpServers\": { \"x\": 1 },\r\n  \"env\": {\"OTHER\":\"z\",},\r\n}\r\n";
        let mut d = Document::decode(raw, true).unwrap();
        let c = d
            .apply(&Edit::set(
                &["env", "ANTHROPIC_BASE_URL"],
                "https://api.anthropic.com",
            ))
            .unwrap();
        let s = String::from_utf8(d.encode()).unwrap();
        assert!(s.contains("\"mcpServers\": { \"x\": 1 }"));
        assert!(s.contains("// keep\r\n"));
        assert!(d.undo(&c).unwrap());
        assert_eq!(d.value().unwrap()["env"]["OTHER"], "z");
    }
    #[test]
    fn reject_duplicate_and_wrong_shape() {
        assert!(Document::decode(br#"{"env":{},"env":{}}"#, true).is_err());
        let mut d = Document::decode(br#"{"env":"bad"}"#, true).unwrap();
        assert!(d.apply(&Edit::set(&["env", "A"], "b")).is_err());
        assert_eq!(d.encode(), br#"{"env":"bad"}"#);
    }
    #[test]
    fn undo_keeps_later_edits_and_conflicts() {
        let mut d = Document::empty(false);
        let c = d.apply(&Edit::set(&["env", "A"], "bridge")).unwrap();
        d.apply(&Edit::set(&["env", "B"], "user")).unwrap();
        assert!(d.undo(&c).unwrap());
        assert_eq!(d.value().unwrap(), serde_json::json!({"env":{"B":"user"}}));
        let c = d.apply(&Edit::set(&["env", "A"], "bridge")).unwrap();
        d.apply(&Edit::set(&["env", "A"], "changed")).unwrap();
        assert!(!d.undo(&c).unwrap());
    }
    #[test]
    fn array_entry_preserves_existing_text() {
        let raw = br#"{"entries":[ {"id":"old",  "name":"User"} ],"appliedId":"old"}"#;
        let mut d = Document::decode(raw, false).unwrap();
        let t = Target::Entry {
            path: vec!["entries".into()],
            id: "bridge".into(),
        };
        let c = d
            .apply(&Edit {
                target: t,
                value: Some(serde_json::json!({"id":"bridge","name":"IKUN Bridge"})),
            })
            .unwrap();
        assert!(String::from_utf8(d.encode())
            .unwrap()
            .contains("{\"id\":\"old\",  \"name\":\"User\"}"));
        assert!(d.undo(&c).unwrap());
        assert_eq!(d.value().unwrap()["entries"].as_array().unwrap().len(), 1);
    }
    #[test]
    fn file_undo_preserves_new_user_values() {
        let temp = tempfile::tempdir().unwrap();
        let p = temp.path().join("settings.json");
        std::fs::write(&p, b"{\"env\": {\"OLD\":1}}\n").unwrap();
        let r = FileRecord::prepare(&p, &[Edit::set(&["env", "A"], "b")], true).unwrap();
        r.write().unwrap();
        let mut d = Document::decode(&std::fs::read(&p).unwrap(), true).unwrap();
        d.apply(&Edit::set(&["env", "NEW"], 2)).unwrap();
        atomic_write(&p, &d.encode()).unwrap();
        assert!(r.rollback().unwrap().is_empty());
        let v = Document::decode(&std::fs::read(p).unwrap(), true)
            .unwrap()
            .value()
            .unwrap();
        assert_eq!(v["env"], serde_json::json!({"OLD":1,"NEW":2}));
    }
    #[test]
    fn utf16_roundtrip() {
        let text = "{\r\n \"中文\": \"😀\"\r\n}";
        let mut raw = vec![0xff, 0xfe];
        for n in text.encode_utf16() {
            raw.extend(n.to_le_bytes())
        }
        let d = Document::decode(&raw, false).unwrap();
        assert_eq!(raw, d.encode());
    }
}
