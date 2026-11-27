use rand::{distributions::Alphanumeric, Rng};
use serde::{Deserialize, Serialize};
use socket2::{Domain, Protocol, Socket, Type};
use std::{
    fs::{self, File},
    io::{self, Read, Write},
    net::{Ipv4Addr, SocketAddrV4},
    path::Path,
};
use tokio::net::TcpListener;
use zeroize::Zeroizing;

const KEY_LEN: usize = 48;

#[derive(Deserialize, Serialize)]
struct Profile {
    port: u16,
    key: String,
}

pub fn new_key() -> String {
    rand::rngs::OsRng
        .sample_iter(Alphanumeric)
        .take(KEY_LEN)
        .map(char::from)
        .collect()
}

pub fn bind(root: &Path) -> Result<(TcpListener, Zeroizing<String>), String> {
    let path = root.join("desktop-profile.bin");
    match File::open(&path) {
        Ok(file) => {
            let mut bytes = Zeroizing::new(Vec::new());
            file.take(4097)
                .read_to_end(&mut bytes)
                .map_err(|_| "无法读取 Desktop 本机接入配置")?;
            if bytes.len() > 4096 {
                return Err("Desktop 本机接入配置无效".into());
            }
            let data = protect(&bytes, false)?;
            let profile: Profile =
                serde_json::from_slice(&data).map_err(|_| "Desktop 本机接入配置无效")?;
            let key = Zeroizing::new(profile.key);
            if profile.port == 0
                || key.len() != KEY_LEN
                || !key.bytes().all(|b| b.is_ascii_alphanumeric())
            {
                return Err("Desktop 本机接入配置无效".into());
            }
            let listener = listen(profile.port)
                .map_err(|_| format!("无法监听本机端口 {}，请检查占用后重试", profile.port))?;
            Ok((listener, key))
        }
        Err(err) if err.kind() == io::ErrorKind::NotFound => {
            let listener = listen(0).map_err(|_| "无法打开本机端口")?;
            let port = listener
                .local_addr()
                .map_err(|_| "无法读取本机端口")?
                .port();
            let profile = Profile {
                port,
                key: new_key(),
            };
            let data = Zeroizing::new(
                serde_json::to_vec(&profile).map_err(|_| "无法生成 Desktop 本机接入配置")?,
            );
            let key = Zeroizing::new(profile.key);
            let protected = protect(&data, true)?;
            fs::create_dir_all(root).map_err(|_| "无法创建 Bridge 配置目录")?;
            write_new(&path, &protected).map_err(|_| "无法保存 Desktop 本机接入配置")?;
            Ok((listener, key))
        }
        Err(_) => Err("无法读取 Desktop 本机接入配置".into()),
    }
}

fn listen(port: u16) -> io::Result<TcpListener> {
    let socket = Socket::new(Domain::IPV4, Type::STREAM, Some(Protocol::TCP))?;
    #[cfg(windows)]
    {
        use std::os::windows::io::AsRawSocket;
        use windows::Win32::Networking::WinSock::{
            setsockopt, WSAGetLastError, SOCKET, SOCKET_ERROR, SOL_SOCKET, SO_EXCLUSIVEADDRUSE,
        };
        let result = unsafe {
            setsockopt(
                SOCKET(socket.as_raw_socket() as usize),
                SOL_SOCKET,
                SO_EXCLUSIVEADDRUSE,
                Some(&1i32.to_ne_bytes()),
            )
        };
        if result == SOCKET_ERROR {
            return Err(io::Error::from_raw_os_error(unsafe { WSAGetLastError() }.0));
        }
    }
    #[cfg(unix)]
    socket.set_reuse_address(true)?;
    socket.bind(&SocketAddrV4::new(Ipv4Addr::LOCALHOST, port).into())?;
    socket.listen(128)?;
    socket.set_nonblocking(true)?;
    TcpListener::from_std(socket.into())
}

pub fn write_new(path: &Path, bytes: &[u8]) -> io::Result<()> {
    let parent = path.parent().ok_or(io::ErrorKind::InvalidInput)?;
    let mut temporary = tempfile::NamedTempFile::new_in(parent)?;
    temporary.write_all(bytes)?;
    temporary.as_file().sync_all()?;
    temporary.persist_noclobber(path).map_err(|err| err.error)?;
    Ok(())
}

#[cfg(windows)]
pub(crate) fn protect(data: &[u8], encrypt: bool) -> Result<Zeroizing<Vec<u8>>, String> {
    use windows::{
        core::w,
        Win32::{
            Foundation::{LocalFree, HLOCAL},
            Security::Cryptography::{
                CryptProtectData, CryptUnprotectData, CRYPTPROTECT_UI_FORBIDDEN, CRYPT_INTEGER_BLOB,
            },
        },
    };
    use zeroize::Zeroize;
    let input = CRYPT_INTEGER_BLOB {
        cbData: data.len() as u32,
        pbData: data.as_ptr().cast_mut(),
    };
    let mut output = CRYPT_INTEGER_BLOB::default();
    let result = unsafe {
        if encrypt {
            CryptProtectData(
                &input,
                w!("IKUN Bridge Desktop"),
                None,
                None,
                None,
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut output,
            )
        } else {
            CryptUnprotectData(
                &input,
                None,
                None,
                None,
                None,
                CRYPTPROTECT_UI_FORBIDDEN,
                &mut output,
            )
        }
    };
    result.map_err(|_| "无法使用 Windows 保护 Desktop 本机密钥")?;
    if output.pbData.is_null() {
        return Err("Desktop 本机密钥无效".into());
    }
    let buffer = unsafe { std::slice::from_raw_parts_mut(output.pbData, output.cbData as usize) };
    let result = Zeroizing::new(buffer.to_vec());
    buffer.zeroize();
    unsafe { LocalFree(Some(HLOCAL(output.pbData.cast()))) };
    Ok(result)
}

#[cfg(not(windows))]
pub(crate) fn protect(data: &[u8], _: bool) -> Result<Zeroizing<Vec<u8>>, String> {
    // NamedTempFile creates the profile with owner-only permissions on Unix.
    Ok(Zeroizing::new(data.to_vec()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn desktop_profile_reuses_loopback_port_and_local_key() {
        let root = tempfile::tempdir().unwrap();
        let (first, key) = bind(root.path()).unwrap();
        let address = first.local_addr().unwrap();
        assert!(address.ip().is_loopback());
        assert!(bind(root.path()).is_err());
        drop(first);
        let (second, restored) = bind(root.path()).unwrap();
        assert_eq!(address, second.local_addr().unwrap());
        assert!(*key == *restored);
        #[cfg(windows)]
        assert!(!fs::read(root.path().join("desktop-profile.bin"))
            .unwrap()
            .windows(KEY_LEN)
            .any(|b| b == restored.as_bytes()));
    }
}
