use anyhow::Result;
use ikun_bridge_lib::{
    config_edit::{Document, Edit, FileRecord},
    settings::{self, Settings},
    store::Store,
};

#[test]
fn certificate_marker_does_not_claim_system_trust() -> Result<()> {
    let directory = tempfile::tempdir()?;
    let mut params = rcgen::CertificateParams::new(vec![])?;
    params.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Constrained(0));
    let key = rcgen::KeyPair::generate()?;
    let cert = params.self_signed(&key)?;
    std::fs::write(directory.path().join("bridge-ca.pem"), cert.pem())?;
    std::fs::write(directory.path().join("certificate-installed"), "1")?;
    assert!(!ikun_bridge_lib::platform::user_certificate_trusted(
        directory.path()
    )?);
    Ok(())
}

#[test]
fn partial_transaction_reverts_first_file_and_keeps_concurrent_edit() -> Result<()> {
    let d = tempfile::tempdir()?;
    let store = Store::new(d.path().join("bridge"))?;
    let a = d.path().join("a.json");
    let b = d.path().join("b.json");
    std::fs::write(&a, b"{\"user\": 1}\n")?;
    std::fs::write(&b, b"{\"value\":1}\n")?;
    let first = FileRecord::prepare(&a, &[Edit::set(&["bridge"], true)], true)?;
    let second = FileRecord::prepare(&b, &[Edit::set(&["value"], 2)], true)?;
    std::fs::write(&b, b"{\"value\":999,\"user\":true}\n")?;
    assert!(store.apply("fixture", vec![first, second]).is_err());
    assert_eq!(std::fs::read(&a)?, b"{\"user\": 1}\n");
    assert_eq!(std::fs::read(&b)?, b"{\"value\":999,\"user\":true}\n");
    assert!(store.journals()?[0].pending);
    Ok(())
}
#[test]
fn desktop_multi_file_recovery_preserves_mcp_and_other_profile() -> Result<()> {
    let d = tempfile::tempdir()?;
    let store = Store::new(d.path().join("bridge"))?;
    let root = d.path().join("Claude-3p");
    std::fs::create_dir_all(root.join("configLibrary"))?;
    let meta = root.join("configLibrary/_meta.json");
    let main = root.join("claude_desktop_config.json");
    let original_main=br#"{ "deploymentMode":"1p", "mcpServers":{ "user":{ "command":"local.exe" } }, "keep":true }"#;
    std::fs::write(
        &meta,
        b"{\"entries\":[ {\"id\":\"old\",  \"name\":\"Original\"} ],\"appliedId\":\"old\"}\n",
    )?;
    std::fs::write(&main, original_main)?;
    let secrets = store.secrets()?;
    let settings = Settings {
        desktop_config_root: root.display().to_string(),
        ..Default::default()
    };
    let records = settings::prepare_desktop(&settings, &secrets)?;
    let journal = store.apply("desktop", records)?;
    assert!(String::from_utf8(std::fs::read(&meta)?)?
        .contains("{\"id\":\"old\",  \"name\":\"Original\"}"));
    let mut doc = Document::decode(&std::fs::read(&main)?, false)?;
    doc.apply(&Edit::set(&["laterUserSetting"], "retain"))?;
    std::fs::write(&main, doc.encode())?;
    assert!(store.undo(&journal)?.is_empty());
    let result = Document::decode(&std::fs::read(&main)?, false)?.value()?;
    assert_eq!(result["deploymentMode"], "1p");
    assert_eq!(result["laterUserSetting"], "retain");
    assert_eq!(result["mcpServers"]["user"]["command"], "local.exe");
    assert!(!root
        .join(format!("configLibrary/{}.json", settings::PROFILE_ID))
        .exists());
    assert_eq!(
        Document::decode(&std::fs::read(meta)?, false)?.value()?["entries"]
            .as_array()
            .unwrap()
            .len(),
        1
    );
    Ok(())
}
#[test]
fn undo_conflict_can_be_resolved_without_overwriting_user_content() -> Result<()> {
    let d = tempfile::tempdir()?;
    let store = Store::new(d.path().join("bridge"))?;
    let p = d.path().join("settings.json");
    std::fs::write(&p, b"{\"env\":{\"URL\":\"original\"}}")?;
    let id = store.apply(
        "cli",
        vec![FileRecord::prepare(
            &p,
            &[Edit::set(&["env", "URL"], "bridge")],
            true,
        )?],
    )?;
    let mut doc = Document::decode(&std::fs::read(&p)?, true)?;
    doc.apply(&Edit::set(&["env", "URL"], "user-change"))?;
    std::fs::write(&p, doc.encode())?;
    assert_eq!(store.undo(&id)?.len(), 1);
    assert_eq!(
        Document::decode(&std::fs::read(&p)?, true)?.value()?["env"]["URL"],
        "user-change"
    );
    assert_eq!(store.journals()?.len(), 1);
    doc.apply(&Edit::set(&["env", "URL"], "bridge"))?;
    std::fs::write(&p, doc.encode())?;
    assert!(store.undo(&id)?.is_empty());
    assert_eq!(
        Document::decode(&std::fs::read(p)?, true)?.value()?["env"]["URL"],
        "original"
    );
    Ok(())
}
#[test]
fn secret_and_backup_not_written_in_plaintext() -> Result<()> {
    let d = tempfile::tempdir()?;
    let store = Store::new(d.path().join("bridge"))?;
    let mut secrets = store.secrets()?;
    secrets.gateway_key = "UPSTREAM_SECRET_CANARY_12345".into();
    store.save_secrets(&secrets)?;
    let encrypted = std::fs::read(store.root.join("secrets.bin"))?;
    assert!(!encrypted
        .windows(secrets.gateway_key.len())
        .any(|w| w == secrets.gateway_key.as_bytes()));
    assert_eq!(store.secrets()?.gateway_key, secrets.gateway_key);
    let p = d.path().join("settings.json");
    std::fs::write(&p, br#"{"userKey":"ORIGINAL_USER_SECRET_67890"}"#)?;
    let id = store.apply(
        "cli",
        vec![FileRecord::prepare(
            &p,
            &[Edit::set(&["bridge"], true)],
            true,
        )?],
    )?;
    let bytes = std::fs::read(store.root.join("undo").join(format!("{id}.bin")))?;
    assert!(!bytes
        .windows(26)
        .any(|w| w == b"ORIGINAL_USER_SECRET_67890"));
    Ok(())
}
#[test]
fn malformed_config_never_creates_a_transaction() -> Result<()> {
    let d = tempfile::tempdir()?;
    let p = d.path().join("settings.json");
    for raw in [b"{\"env\":1".as_slice(), br#"{"env":{},"\u0065nv":{}}"#] {
        std::fs::write(&p, raw)?;
        assert!(FileRecord::prepare(&p, &[Edit::set(&["env", "URL"], "bridge")], true).is_err());
        assert_eq!(std::fs::read(&p)?, raw);
    }
    Ok(())
}

#[test]
fn never_clobbers_a_new_file_created_after_preflight() -> Result<()> {
    let d = tempfile::tempdir()?;
    let path = d.path().join("settings.json");
    let record = FileRecord::prepare(
        &path,
        &[Edit::set(&["env", "DISABLE_TELEMETRY"], "1")],
        true,
    )?;
    let authored = br#"{"env": {"SAVED_SETTING": "KEEP"}}"#;
    std::fs::write(&path, authored)?;
    assert!(record.write().is_err());
    assert_eq!(std::fs::read(path)?, authored);
    Ok(())
}
