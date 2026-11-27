fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.get(1).map(String::as_str) == Some("tcp-probe") && args.len() == 4 {
        let address = std::net::SocketAddr::new(args[2].parse().unwrap(), args[3].parse().unwrap());
        let outcome =
            std::net::TcpStream::connect_timeout(&address, std::time::Duration::from_secs(2));
        println!(
            "{}",
            serde_json::json!({"connected":outcome.is_ok(),"error":outcome.err().map(|e|e.raw_os_error())})
        );
        return;
    }
    println!("IKUN Bridge {}", env!("CARGO_PKG_VERSION"));
}
