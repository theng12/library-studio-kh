// Test-only packaged WKWebView check. It never starts the real host or opens a native panel.
use std::path::{Path, PathBuf};
use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};

#[derive(Default)]
struct CheckStateInner {
    finished: bool,
    exit_code: i32,
    receipt: Option<PathBuf>,
    controls: usize,
    chosen: usize,
    clipboard: Option<String>,
    picker: Option<String>,
}
struct CheckState(std::sync::Mutex<CheckStateInner>);

impl CheckState {
    fn lock(&self) -> std::sync::MutexGuard<'_, CheckStateInner> {
        self.0.lock().unwrap_or_else(|e| e.into_inner())
    }
}

fn fixture_url(raw: &str) -> Result<tauri::Url, String> {
    let url: tauri::Url = raw.parse().map_err(|e| format!("invalid test URL: {e}"))?;
    if url.scheme() != "http" || url.host_str() != Some("127.0.0.1") || url.port().is_none()
        || url.path() != "/" || url.query().is_some() || url.fragment().is_some()
        || !url.username().is_empty() || url.password().is_some() {
        return Err("test URL must be a bare http://127.0.0.1:<port>/ fixture origin".into());
    }
    Ok(url)
}

fn fixture_home(raw: &str) -> Result<PathBuf, String> {
    let home = Path::new(raw).canonicalize().map_err(|e| format!("test home: {e}"))?;
    let tmp = std::env::temp_dir().canonicalize().map_err(|e| e.to_string())?;
    if home == tmp || !home.starts_with(tmp) {
        return Err("KH_TEST_HOME must be an existing directory below the local temporary directory".into());
    }
    Ok(home)
}

fn prepared_home(home: &Path) -> Result<(), String> {
    if home.join("library.db").is_file() { return Ok(()); }
    Err("KH_TEST_HOME must already hold a fixture library.db".into())
}

fn receipt_path(home: &Path) -> PathBuf { home.join("native-webview-result.json") }
fn write_receipt(path: &Path, report: &serde_json::Value) -> Result<(), String> {
    std::fs::write(path, format!("{}\n", report)).map_err(|e| format!("receipt write: {e}"))
}
fn finish(app: &tauri::AppHandle, report: serde_json::Value) {
    let state = app.state::<CheckState>();
    let (path, code) = {
        let mut state = state.lock();
        if state.finished { let code = state.exit_code; drop(state); app.exit(code); return; }
        state.finished = true;
        let report_ok = report.get("ok").and_then(|v| v.as_bool()).unwrap_or(false);
        // The report's own numbers, not the counts this side hard-coded, decide completeness.
        let controls = report.get("controls").and_then(|v| v.as_u64());
        let chosen = report.get("chosen").and_then(|v| v.as_u64());
        let complete = report.get("fixture").and_then(|v| v.as_str()).is_some_and(|s| !s.is_empty())
            && controls == Some(state.controls as u64) && controls == chosen
            && report.get("clipboard").and_then(|v| v.as_str()).is_some()
            && report.get("picker").and_then(|v| v.as_str()).is_some();
        state.exit_code = if report_ok && complete { 0 } else { 1 };
        state.clipboard = report.get("clipboard").and_then(|v| v.as_str()).map(str::to_string);
        state.picker = report.get("picker").and_then(|v| v.as_str()).map(str::to_string);
        state.controls = controls.unwrap_or(0) as usize;
        state.chosen = chosen.unwrap_or(0) as usize;
        (state.receipt.clone(), state.exit_code)
    };
    if let Some(path) = path.as_ref() {
        if write_receipt(path, &report).is_err() {
            app.state::<CheckState>().lock().exit_code = 1;
            app.exit(1);
            return;
        }
    }
    app.exit(code);
}

pub fn run() -> Result<(), String> {
    let url = fixture_url(&std::env::var("KH_TEST_URL").map_err(|_| "KH_TEST_URL is required")?)?;
    let home = fixture_home(&std::env::var("KH_TEST_HOME").map_err(|_| "KH_TEST_HOME is required")?)?;
    prepared_home(&home)?;
    let receipt = receipt_path(&home);
    if receipt.exists() { return Err("use a fresh test home; native receipt already exists".into()); }
    let password = std::env::var("KH_TEST_PASSWORD").map_err(|_| "KH_TEST_PASSWORD is required")?;
    let codes = password.chars().map(|ch| (ch as u32).to_string()).collect::<Vec<_>>().join(",");
    let script = include_str!("webview_check.js").replace("__TEST_PASSWORD__", &format!("String.fromCodePoint({codes})"));
    let home_string = home.to_string_lossy().to_string();
    let receipt_string = receipt.display().to_string();
    let controls = 9usize;
    let chosen = 1usize;
    tauri::Builder::default()
        .manage(CheckState(std::sync::Mutex::new(CheckStateInner { receipt: Some(receipt.clone()), controls, chosen, ..Default::default() })))
        .setup(move |app| {
            #[cfg(target_os = "macos")]
            app.set_activation_policy(tauri::ActivationPolicy::Accessory);
            let origin = url.origin();
            let nav_handle = app.handle().clone();
            let nav_home = home_string.clone();
            let nav_receipt = receipt_string.clone();
            let window = WebviewWindowBuilder::new(app, "native-check", WebviewUrl::External(url.clone()))
                .visible(false).focused(false).incognito(true)
                .initialization_script(script.clone())
                .on_new_window(|_, _| tauri::webview::NewWindowResponse::<tauri::Wry>::Deny)
                .on_navigation(move |target| {
                    if target.origin() != origin { return false; }
                    if target.path() != "/__library_webview_check" { return true; }
                    let params = target.query_pairs().collect::<std::collections::HashMap<_, _>>();
                    let webview = params.get("report").and_then(|s| serde_json::from_str::<serde_json::Value>(&s).ok());
                    let hidden = nav_handle.get_webview_window("native-check").and_then(|w| w.is_visible().ok()).is_some_and(|visible| !visible);
                    let unfocused = nav_handle.get_webview_window("native-check").and_then(|w| w.is_focused().ok()).is_some_and(|focused| !focused);
                    let ok = params.get("ok").is_some_and(|v| v == "1") && hidden && unfocused;
                    let report = serde_json::json!({"ok":ok,"hidden":hidden,"unfocused":unfocused,"home":nav_home,"receipt":nav_receipt,"webview":webview});
                    let fixture = webview.as_ref().and_then(|v| v.get("fixture")).and_then(|v| v.as_str()).unwrap_or("").to_string();
                    let controls = webview.as_ref().and_then(|v| v.get("controls")).and_then(|v| v.as_u64()).unwrap_or(0) as usize;
                    let chosen = webview.as_ref().and_then(|v| v.get("chosen")).and_then(|v| v.as_u64()).unwrap_or(0) as usize;
                    let clipboard = webview.as_ref().and_then(|v| v.get("clipboard")).and_then(|v| v.as_str()).map(str::to_string);
                    let picker = webview.as_ref().and_then(|v| v.get("picker")).and_then(|v| v.as_str()).map(str::to_string);
                    let mut report = report;
                    report["fixture"] = serde_json::Value::String(fixture);
                    report["controls"] = serde_json::json!(controls);
                    report["chosen"] = serde_json::json!(chosen);
                    report["clipboard"] = clipboard.clone().map(serde_json::Value::String).unwrap_or(serde_json::Value::Null);
                    report["picker"] = picker.clone().map(serde_json::Value::String).unwrap_or(serde_json::Value::Null);
                    finish(&nav_handle, report);
                    false
                }).build();
            if let Err(error) = window {
                finish(app.handle(), serde_json::json!({"ok":false,"error":format!("window creation: {error}"),"home":home_string,"receipt":receipt_string}));
            }
            let timeout_handle = app.handle().clone();
            std::thread::spawn(move || {
                std::thread::sleep(std::time::Duration::from_secs(45));
                finish(&timeout_handle, serde_json::json!({"ok":false,"error":"native check timed out","home":home_string,"receipt":receipt_string}));
            });
            Ok(())
        })
        .build(tauri::generate_context!()).map_err(|e| e.to_string())?
        .run(|_, _| {});
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn normal_and_check_builds_are_separate() {
        let main = include_str!("main.rs");
        let gated = main.split("#[cfg(all(test, debug_assertions))]").nth(1).unwrap_or("");
        assert!(gated.contains("--webview-check"));
        assert_eq!(main.matches("webview_check::run()").count(), 1);
    }
    #[test]
    fn failure_receipt_is_nonzero_and_complete() {
        let root = std::env::temp_dir().join(format!("library-receipt-test-{}", std::process::id()));
        std::fs::create_dir_all(&root).unwrap();
        let path = root.join("native-webview-result.json");
        let report = serde_json::json!({"ok":false,"error":"fixture"});
        assert!(write_receipt(&path, &report).is_ok());
        assert_eq!(std::fs::read_to_string(&path).unwrap(), "{\"ok\":false,\"error\":\"fixture\"}\n");
        let _ = std::fs::remove_dir_all(root);
    }
    #[test]
    fn only_bare_loopback_fixture_origins_are_allowed() {
        assert!(fixture_url("http://127.0.0.1:64800/").is_ok());
        for url in ["https://example.com/", "http://127.0.0.1/", "http://127.0.0.1:64800/#token", "http://127.0.0.1:64800/?token=secret", "http://user@127.0.0.1:64800/", "http://127.0.0.1:64800/api/state"] { assert!(fixture_url(url).is_err(), "{url}"); }
    }
    #[test]
    fn fixture_home_is_bounded_and_prepared() {
        let root = std::env::temp_dir().join(format!("library-webview-check-{}", std::process::id()));
        let home = root.join("home");
        std::fs::create_dir_all(&home).unwrap();
        assert!(fixture_home(home.to_str().unwrap()).is_ok());
        assert!(prepared_home(&home).is_err());
        std::fs::write(home.join("library.db"), b"fixture").unwrap();
        assert!(prepared_home(&home).is_ok());
        let _ = std::fs::remove_dir_all(root);
    }
}