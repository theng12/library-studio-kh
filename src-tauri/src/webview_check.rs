// Opt-in packaged WKWebView check: no live sidecar, host control or visible window.
use std::path::{Path, PathBuf};
use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};

fn fixture_url(raw: &str) -> Result<tauri::Url, String> {
    let url: tauri::Url = raw.parse().map_err(|e| format!("invalid test URL: {e}"))?;
    if url.scheme() != "http" || url.host_str() != Some("127.0.0.1")
        || url.port().is_none() || url.path() != "/" || url.query().is_some()
        || url.fragment().is_some() || !url.username().is_empty() || url.password().is_some()
    {
        return Err("test URL must be a bare http://127.0.0.1:<port>/ fixture origin".into());
    }
    Ok(url)
}

fn receipt_path(home: &Path) -> Result<PathBuf, String> {
    let home = home.canonicalize().map_err(|e| format!("test home: {e}"))?;
    let tmp = std::env::temp_dir().canonicalize().map_err(|e| e.to_string())?;
    if home == tmp || !home.starts_with(tmp) {
        return Err("KH_TEST_HOME must be an existing directory below the local temporary directory".into());
    }
    Ok(home.join("native-webview-result.json"))
}

pub fn run() -> Result<(), String> {
    let url = fixture_url(&std::env::var("KH_TEST_URL").map_err(|_| "KH_TEST_URL is required")?)?;
    let receipt = receipt_path(Path::new(&std::env::var("KH_TEST_HOME").map_err(|_| "KH_TEST_HOME is required")?))?;
    if receipt.exists() { return Err("use a fresh test home; native receipt already exists".into()); }
    let password = std::env::var("KH_TEST_PASSWORD").map_err(|_| "KH_TEST_PASSWORD is required")?;
    let codes = password.chars().map(|ch| (ch as u32).to_string()).collect::<Vec<_>>().join(",");
    let script = include_str!("webview_check.js").replace("__TEST_PASSWORD__", &format!("String.fromCodePoint({codes})"));
    tauri::Builder::default()
        .setup(move |app| {
            #[cfg(target_os = "macos")]
            app.set_activation_policy(tauri::ActivationPolicy::Accessory);
            let handle = app.handle().clone();
            let origin = url.origin();
            let output = receipt.clone();
            WebviewWindowBuilder::new(app, "native-check", WebviewUrl::External(url.clone()))
                .visible(false).focused(false).incognito(true)
                .initialization_script(script.clone())
                .on_new_window(|_, _| tauri::webview::NewWindowResponse::<tauri::Wry>::Deny)
                .on_navigation(move |target| {
                    if target.origin() != origin { return false; }
                    if target.path() != "/__library_webview_check" { return true; }
                    let params = target.query_pairs().collect::<std::collections::HashMap<_, _>>();
                    let report = params.get("report").map(|s| s.as_ref()).unwrap_or("null");
                    let window = handle.get_webview_window("native-check");
                    let hidden = window.as_ref().is_some_and(|w| w.is_visible().ok() == Some(false));
                    let unfocused = window.as_ref().is_some_and(|w| w.is_focused().ok() == Some(false));
                    let ok = params.get("ok").is_some_and(|v| v == "1") && hidden && unfocused;
                    let result = format!("{{\"ok\":{ok},\"hidden\":{hidden},\"unfocused\":{unfocused},\"webview\":{report}}}\n");
                    let written = std::fs::write(&output, result).is_ok();
                    eprintln!("native-webview-check: {}", if ok && written { "PASS" } else { "FAIL" });
                    handle.exit(if ok && written { 0 } else { 1 });
                    false
                }).build()?;
            let handle = app.handle().clone();
            let output = receipt.clone();
            std::thread::spawn(move || {
                std::thread::sleep(std::time::Duration::from_secs(45));
                if !output.exists() {
                    let _ = std::fs::write(output, "{\"ok\":false,\"error\":\"native check timed out\"}\n");
                    eprintln!("native-webview-check: FAIL (timeout)");
                    handle.exit(1);
                }
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
    fn visible_main_window_starts_only_after_normal_setup() {
        let visible = std::env::var("KH_TEST_URL").is_err();
        assert!(visible);
        let source = include_str!("webview_check.rs");
        assert!(source.contains("let visible = std::env::var(\"KH_TEST_URL\").is_err();"));
        assert!(source.contains(".visible(visible).focused(visible).incognito(!visible)"));
        assert!(source.contains("if !visible { app.set_activation_policy(tauri::ActivationPolicy::Accessory); }"));
        assert!(source.contains("WindowUrl::new(url.clone(), visible)"));
        assert!(source.contains("WebviewWindowBuilder::new(app, \"main\""));
        assert!(source.contains("#[cfg(not(debug_assertions))]"));
    }
    #[test]
    fn only_bare_loopback_fixture_origins_are_allowed() {
        assert!(fixture_url("http://127.0.0.1:64800/").is_ok());
        for url in ["https://example.com/", "http://127.0.0.1/", "http://127.0.0.1:64800/#token", "http://127.0.0.1:64800/?token=secret", "http://user@127.0.0.1:64800/", "http://127.0.0.1:64800/api/state"] {
            assert!(fixture_url(url).is_err(), "{url}");
        }
    }
    #[test]
    fn live_home_and_temp_root_are_refused() {
        assert!(receipt_path(Path::new(&std::env::var("HOME").unwrap())).is_err());
        assert!(receipt_path(&std::env::temp_dir()).is_err());
    }
}
