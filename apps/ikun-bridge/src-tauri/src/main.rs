#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
fn main() {
    if let Some(code) = ikun_bridge_lib::platform::guard_helper() {
        std::process::exit(code)
    }
    ikun_bridge_lib::app::run();
}
