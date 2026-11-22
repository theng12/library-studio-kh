// Feature-gated packaged WKWebView check, compiled only with --features webview-check
// and never present in the shipped app. It never starts the real host, never stops the
// owner's host, never opens a native panel and never spawns a child process.
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::OnceLock;
use sha2::{Digest, Sha256};
use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};

const DEFAULT_TIMEOUT_SECS: u64 = 90;
const CONTROLS: usize = 11;

// The environment whose values Tauri/WebKit derive data directories from.
const ISOLATED_ENV: [(&str, &str); 6] = [
    ("HOME", "home"),
    ("XDG_CONFIG_HOME", "config"),
    ("XDG_DATA_HOME", "data"),
    ("XDG_CACHE_HOME", "cache"),
    ("XDG_STATE_HOME", "state"),
    ("TMPDIR", "tmp"),
];

/// Everything one run must prove: run identity, fixture binding, and the whole-run bound.
struct Expect {
    nonce: String,
    url: String,    // raw KH_TEST_URL, already validated as a bare loopback origin
    home: PathBuf,  // canonical fixture home, below the local temporary directory
    work: PathBuf,  // fixture work dir (home's parent): home, downloads, library
    receipt: PathBuf,
    password: String,
    controls: usize,
    timeout_secs: u64,
    deadline: std::time::Instant, // construction and webview phases share one bound
}

static START: OnceLock<Expect> = OnceLock::new();
// Construction (app_context through Builder::build, including window/WebKit work)
// is finished when this is set; before it, the construction watchdog owns the run.
static CONSTRUCTED: AtomicBool = AtomicBool::new(false);

#[derive(Default)]
struct CheckStateInner {
    finished: bool,
    exit_code: i32,
    receipt: Option<PathBuf>,
}
struct CheckState(std::sync::Mutex<CheckStateInner>);

impl CheckState {
    fn lock(&self) -> std::sync::MutexGuard<'_, CheckStateInner> {
        self.0.lock().unwrap_or_else(|e| e.into_inner())
    }
}

fn required_env(name: &str) -> Result<String, String> {
    std::env::var(name).map_err(|_| format!("{name} is required"))
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

// A prepared home is a precondition so a half-started fixture fails fast. It is
// never proof that the fixture URL serves this home: proof is the run nonce plus
// the served localUrl and settings paths checked in webview_report_complete.
fn prepared_home(home: &Path) -> Result<(), String> {
    if home.join("settings.json").is_file() { return Ok(()); }
    Err("KH_TEST_HOME must already hold a fixture settings.json".into())
}

fn receipt_path(home: &Path) -> PathBuf { home.join("native-webview-result.json") }
fn startup_path(home: &Path) -> PathBuf { home.join("native-webview-startup.json") }

fn write_receipt(path: &Path, report: &serde_json::Value) -> Result<(), String> {
    std::fs::write(path, format!("{}\n", report)).map_err(|e| format!("receipt write: {e}"))
}

fn write_startup_marker(path: &Path, marker: &serde_json::Value) -> Result<(), String> {
    std::fs::write(path, format!("{marker}\n")).map_err(|e| format!("startup marker write: {e}"))
}

fn run_nonce() -> Result<String, String> {
    use std::io::Read;
    let mut buf = [0u8; 16];
    std::fs::File::open("/dev/urandom")
        .and_then(|mut file| file.read_exact(&mut buf))
        .map_err(|e| format!("run identity: {e}"))?;
    Ok(buf.iter().map(|b| format!("{b:02x}")).collect())
}

fn parse_timeout(raw: &str) -> Option<u64> {
    raw.parse::<u64>().ok().filter(|secs| (5..=600).contains(secs))
}

// The bound comes from the runner's environment, never a hidden constant. It is a
// belt only: the runner must still bound the child PID from spawn through wait(),
// because a hang before this watchdog even arms would otherwise leave no stop.
fn watchdog_timeout() -> u64 {
    std::env::var("KH_WEBVIEW_CHECK_TIMEOUT")
        .ok()
        .and_then(|raw| parse_timeout(raw.trim()).or(Some(DEFAULT_TIMEOUT_SECS)))
        .unwrap_or(DEFAULT_TIMEOUT_SECS)
}

fn sha256_file(path: &Path) -> Result<String, String> {
    use std::io::Read;
    let mut file = std::fs::File::open(path).map_err(|e| format!("executable hash: {e}"))?;
    let mut hasher = Sha256::new();
    let mut buf = [0u8; 65536];
    loop {
        let n = file.read(&mut buf).map_err(|e| format!("executable hash: {e}"))?;
        if n == 0 { break; }
        hasher.update(&buf[..n]);
    }
    Ok(hasher.finalize().iter().map(|b| format!("{b:02x}")).collect())
}

fn build_flags() -> serde_json::Value {
    serde_json::json!({
        "version": option_env!("CARGO_PKG_VERSION").unwrap_or("unknown"),
        "profile": if cfg!(debug_assertions) { "debug" } else { "release" },
        "debugAssertions": cfg!(debug_assertions),
        "features": {
            "webview-check": cfg!(feature = "webview-check"),
            "custom-protocol": cfg!(feature = "custom-protocol"),
        },
    })
}

#[allow(clippy::too_many_arguments)]
fn startup_marker(
    nonce: &str,
    exe: &Path,
    exe_sha256: &str,
    env_actual: serde_json::Map<String, serde_json::Value>,
    origin: &str,
    home: &Path,
    started_at_unix: u64,
) -> serde_json::Value {
    serde_json::json!({
        "kind": "native-webview-startup",
        "runId": nonce,
        "pid": std::process::id(),
        "exe": exe.display().to_string(),
        "exeSha256": exe_sha256,
        "build": build_flags(),
        "env": env_actual,
        "fixture": {"origin": origin, "home": home.display().to_string()},
        "startedAtUnix": started_at_unix,
    })
}

/// Validate the fixture, isolate HOME/XDG/tmp into run-owned directories, write
/// the startup marker, and arm the construction watchdog — all strictly before
/// app_context()/WebKit/Tauri setup, so even a startup hang leaves entry evidence.
pub fn start() -> Result<(), String> {
    let url_raw = required_env("KH_TEST_URL")?;
    fixture_url(&url_raw)?;
    let home = fixture_home(&required_env("KH_TEST_HOME")?)?;
    let password = required_env("KH_TEST_PASSWORD")?;
    prepared_home(&home)?;
    let receipt = receipt_path(&home);
    let startup = startup_path(&home);
    if receipt.exists() || startup.exists() {
        return Err("fresh test home required: remove native-webview-startup.json and \
            native-webview-result.json first (or start a new fixture)"
            .into());
    }
    let work = home.parent().ok_or("fixture home has no parent")?.to_path_buf();
    let nonce = run_nonce()?;
    let timeout_secs = watchdog_timeout();
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(timeout_secs);

    // Actual isolation, not a recorded hope: every path Tauri/WebKit derives from
    // the environment is created and set before app_context() can read it.
    let env_root = work.join(format!("native-check-env-{nonce}"));
    let mut env_actual = serde_json::Map::new();
    for (var, name) in ISOLATED_ENV {
        let path = env_root.join(name);
        std::fs::create_dir_all(&path).map_err(|e| format!("isolated {var}: {e}"))?;
        std::env::set_var(var, &path);
        let actual = std::env::var(var).map_err(|e| format!("{var} isolation: {e}"))?;
        if actual != path.to_string_lossy() {
            return Err(format!(
                "{var} did not stay isolated (set {}, read {actual})",
                path.display()
            ));
        }
        env_actual.insert(var.to_string(), serde_json::Value::String(actual));
    }

    let exe = std::env::current_exe().map_err(|e| format!("executable: {e}"))?;
    let exe_sha256 = sha256_file(&exe)?;
    let started_at_unix = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let marker = startup_marker(&nonce, &exe, &exe_sha256, env_actual, &url_raw, &home, started_at_unix);
    write_startup_marker(&startup, &marker)?;

    START.set(Expect {
        nonce,
        url: url_raw,
        home,
        work,
        receipt,
        password,
        controls: CONTROLS,
        timeout_secs,
        deadline,
    })
    .map_err(|_| "this process already started a check".to_string())?;
    spawn_construction_watchdog();
    Ok(())
}

/// Covers construction itself: window/WebKit work can hang before any in-app
/// timer exists, so this fires from the shared deadline with a failure receipt.
/// The runner's external PID timeout stays the mandatory outer bound.
fn spawn_construction_watchdog() {
    let Some(expect) = START.get() else { return };
    let receipt = expect.receipt.clone();
    let run_id = expect.nonce.clone();
    let origin = expect.url.clone();
    let home = expect.home.display().to_string();
    let secs = expect.timeout_secs;
    let wait = expect.deadline.saturating_duration_since(std::time::Instant::now());
    std::thread::spawn(move || {
        std::thread::sleep(wait);
        if CONSTRUCTED.load(Ordering::SeqCst) { return; }
        let report = serde_json::json!({
            "ok": false,
            "phase": "construction",
            "error": format!(
                "startup watchdog: webview construction did not complete within {secs}s; \
                 the runner's external PID bound must still stop the child and record its status"
            ),
            "runId": run_id,
            "fixtureOrigin": origin,
            "fixtureHome": home,
            "receipt": receipt.display().to_string(),
        });
        let _ = write_receipt(&receipt, &report);
        eprintln!(
            "native-webview-check: FAIL (startup watchdog after {secs}s; receipt: {})",
            receipt.display()
        );
        std::process::exit(1);
    });
}

fn field<'a>(w: &'a serde_json::Value, path: &[&str]) -> Option<&'a serde_json::Value> {
    let mut cur = w;
    for key in path { cur = cur.get(*key)?; }
    Some(cur)
}

fn str_at<'a>(w: &'a serde_json::Value, path: &[&str]) -> Option<&'a str> {
    field(w, path).and_then(|v| v.as_str())
}

/// A partial report never passes. Run identity, fixture binding, control counts,
/// a demonstrated clipboard write/readback, and exact folder-picker equality must
/// all hold; feedback messages, file presence and family labels prove nothing.
fn webview_report_complete(w: &serde_json::Value, expect: &Expect) -> bool {
    let under_work = |raw: Option<&str>| -> bool {
        raw.and_then(|p| std::fs::canonicalize(p).ok())
            .is_some_and(|p| p.starts_with(&expect.work))
    };
    let nonce_ok = str_at(w, &["nonce"]) == Some(expect.nonce.as_str());
    let binding_ok = str_at(w, &["fixtureBinding", "localUrl"]) == Some(expect.url.as_str())
        && under_work(str_at(w, &["fixtureBinding", "downloadPath"]))
        && under_work(str_at(w, &["fixtureBinding", "libraryPath"]));
    let controls = field(w, &["controls"]).and_then(|v| v.as_u64());
    let chosen = field(w, &["chosen"]).and_then(|v| v.as_u64());
    // Every path control offers a chooser and accepts a typed path (owner, 2026-11-15).
    let typeable = field(w, &["typeable"]).and_then(|v| v.as_u64());
    let counts_ok = controls == Some(expect.controls as u64)
        && controls == chosen
        && typeable == Some(expect.controls as u64);
    let clipboard_ok = str_at(w, &["clipboard", "state"]) == Some("ok")
        && field(w, &["clipboard", "readbackOk"]).and_then(|v| v.as_bool()) == Some(true);
    let folder_ok = field(w, &["folder", "exact"]).and_then(|v| v.as_bool()) == Some(true)
        && field(w, &["folder", "typeable"]).and_then(|v| v.as_bool()) == Some(true)
        && under_work(str_at(w, &["folder", "chosen"]));
    let picker_ok = str_at(w, &["picker"]).is_some_and(|s| !s.is_empty());
    nonce_ok && binding_ok && counts_ok && clipboard_ok && folder_ok && picker_ok
}

fn finish(app: &tauri::AppHandle, report: serde_json::Value) {
    let state = app.state::<CheckState>();
    let (path, code) = {
        let mut state = state.lock();
        if state.finished { let code = state.exit_code; drop(state); app.exit(code); return; }
        state.finished = true;
        let report_ok = report.get("ok").and_then(|v| v.as_bool()).unwrap_or(false);
        let complete = report
            .get("webview")
            .is_some_and(|w| START.get().is_some_and(|expect| webview_report_complete(w, expect)));
        state.exit_code = if report_ok && complete { 0 } else { 1 };
        (state.receipt.clone(), state.exit_code)
    };
    let mut report = report;
    if let Some(expect) = START.get() {
        report["runId"] = serde_json::json!(expect.nonce);
        report["fixtureOrigin"] = serde_json::json!(expect.url);
        report["fixtureHome"] = serde_json::json!(expect.home.display().to_string());
    }
    if let Some(path) = path.as_ref() {
        if write_receipt(path, &report).is_err() {
            app.state::<CheckState>().lock().exit_code = 1;
            app.exit(1);
            return;
        }
    }
    app.exit(code);
}

pub fn run(context: tauri::Context<tauri::Wry>) -> Result<(), String> {
    let expect = START
        .get()
        .ok_or("startup marker was not written; refusing to start the check")?;
    let url = fixture_url(&expect.url)?;
    let codes = expect
        .password
        .chars()
        .map(|ch| (ch as u32).to_string())
        .collect::<Vec<_>>()
        .join(",");
    let script = include_str!("webview_check.js")
        .replace("__TEST_PASSWORD__", &format!("String.fromCodePoint({codes})"))
        .replace("__TEST_NONCE__", &expect.nonce);
    let home_string = expect.home.to_string_lossy().to_string();
    let receipt_string = expect.receipt.display().to_string();
    let deadline = expect.deadline;
    let built = tauri::Builder::default()
        .manage(CheckState(std::sync::Mutex::new(CheckStateInner {
            receipt: Some(expect.receipt.clone()),
            ..Default::default()
        })))
        .setup(move |app| {
            // Keep the normal app's activation policy: the packaged app does not use Accessory mode.
            let origin = url.origin();
            let nav_handle = app.handle().clone();
            let nav_home = home_string.clone();
            let nav_receipt = receipt_string.clone();
            // The owner approved a brief visible window on this Mac so the clipboard
            // grant can be measured against a focused page. Off unless asked for.
            let visible_mode = std::env::var("LIBRARY_CHECK_VISIBLE").is_ok_and(|v| v == "1");
            let window = WebviewWindowBuilder::new(app, "native-check", WebviewUrl::External(url.clone()))
                .visible(visible_mode).focused(visible_mode).incognito(true)
                .initialization_script(script.clone())
                .on_new_window(|_, _| tauri::webview::NewWindowResponse::<tauri::Wry>::Deny)
                .on_navigation(move |target| {
                    if target.origin() != origin { return false; }
                    if target.path() != "/__library_webview_check" { return true; }
                    let params = target.query_pairs().collect::<std::collections::HashMap<_, _>>();
                    let webview = params.get("report").and_then(|s| serde_json::from_str::<serde_json::Value>(&s).ok());
                    let hidden = nav_handle.get_webview_window("native-check").and_then(|w| w.is_visible().ok()).is_some_and(|visible| !visible);
                    let unfocused = nav_handle.get_webview_window("native-check").and_then(|w| w.is_focused().ok()).is_some_and(|focused| !focused);
                    let ok = params.get("ok").is_some_and(|v| v == "1")
                        && if visible_mode { !hidden } else { hidden && unfocused };
                    // The receipt keeps the page's raw report: every distinguished
                    // field (nonce, binding, clipboard, folder) stays as evidence.
                    let report = serde_json::json!({
                        "ok": ok,
                        "hidden": hidden,
                        "unfocused": unfocused,
                        "visibleMode": visible_mode,
                        "home": nav_home,
                        "receipt": nav_receipt,
                        "webview": webview.unwrap_or(serde_json::Value::Null),
                    });
                    finish(&nav_handle, report);
                    false
                }).build();
            if let Err(error) = window {
                finish(app.handle(), serde_json::json!({"ok":false,"phase":"construction","error":format!("window creation: {error}"),"home":home_string,"receipt":receipt_string}));
            }
            let timeout_handle = app.handle().clone();
            std::thread::spawn(move || {
                std::thread::sleep(deadline.saturating_duration_since(std::time::Instant::now()));
                finish(&timeout_handle, serde_json::json!({"ok":false,"phase":"webview","error":"native check timed out","home":home_string,"receipt":receipt_string}));
            });
            Ok(())
        })
        .build(context);
    // Construction finished either way; from here the in-app timer owns the run.
    CONSTRUCTED.store(true, Ordering::SeqCst);
    let app = match built {
        Ok(app) => app,
        Err(error) => {
            let message = format!("tari build: {error}");
            if let Some(expect) = START.get() {
                let report = serde_json::json!({
                    "ok": false,
                    "phase": "construction",
                    "error": message,
                    "runId": expect.nonce,
                    "fixtureOrigin": expect.url,
                    "fixtureHome": expect.home.display().to_string(),
                });
                let _ = write_receipt(&expect.receipt, &report);
            }
            return Err(message);
        }
    };
    app.run(|_, _| {});
    Ok(())
}

// Window lifecycle check (--window-lifecycle-check): a real, hidden, unfocused
// window runs the shipped close and reopen paths. Closing must hide the same
// window rather than quitting, the menu action must show it again, and the update
// hold must still answer while no window is visible. Never packaged.
fn finish_lifecycle(app: &tauri::AppHandle, report: serde_json::Value) {
    let state = app.state::<CheckState>();
    let (path, code) = {
        let mut state = state.lock();
        if state.finished {
            let code = state.exit_code;
            drop(state);
            app.exit(code);
            return;
        }
        state.finished = true;
        state.exit_code = if report.get("ok").and_then(|v| v.as_bool()).unwrap_or(false) { 0 } else { 1 };
        (state.receipt.clone(), state.exit_code)
    };
    let mut report = report;
    if let Some(expect) = START.get() {
        report["runId"] = serde_json::json!(expect.nonce);
        report["fixtureOrigin"] = serde_json::json!(expect.url);
        report["fixtureHome"] = serde_json::json!(expect.home.display().to_string());
    }
    if let Some(path) = path.as_ref() {
        if write_receipt(path, &report).is_err() {
            app.exit(1);
            return;
        }
    }
    app.exit(code);
}

/// The window's visibility and URL, or nothing when it no longer exists.
fn window_state(app: &tauri::AppHandle) -> (bool, Option<String>) {
    match app.get_webview_window("main") {
        Some(window) => (
            window.is_visible().unwrap_or(false),
            window.url().ok().map(|url| url.to_string()),
        ),
        None => (false, None),
    }
}

/// One real hold request against the fixture's published address, the same call
/// the installer makes. The token comes from the fixture's own 0600 file.
fn hold_request() -> Result<u16, String> {
    use std::io::{Read, Write};
    use std::net::{TcpStream, ToSocketAddrs};
    let url = std::env::var("KH_HOLD_URL").map_err(|_| "KH_HOLD_URL is required".to_string())?;
    let token_file = std::env::var("KH_HOLD_TOKEN_FILE")
        .map_err(|_| "KH_HOLD_TOKEN_FILE is required with KH_HOLD_URL".to_string())?;
    let token = std::fs::read_to_string(&token_file).map_err(|e| format!("hold token: {e}"))?;
    let parsed: tauri::Url = url.parse().map_err(|e| format!("hold URL: {e}"))?;
    let host = parsed.host_str().ok_or("hold URL has no host")?.to_string();
    let port = parsed.port().ok_or("hold URL has no port")?;
    let addr = (host.as_str(), port)
        .to_socket_addrs()
        .map_err(|e| format!("hold address: {e}"))?
        .next()
        .ok_or("hold address did not resolve")?;
    let mut stream = TcpStream::connect_timeout(&addr, std::time::Duration::from_secs(5))
        .map_err(|e| format!("hold connect: {e}"))?;
    stream.set_read_timeout(Some(std::time::Duration::from_secs(10))).ok();
    write!(
        stream,
        "POST {} HTTP/1.1\r\nHost: {}:{}\r\nX-Hub-Token: {}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n",
        parsed.path(),
        host,
        port,
        token.trim()
    )
    .map_err(|e| format!("hold request: {e}"))?;
    let mut response = String::new();
    stream.read_to_string(&mut response).map_err(|e| format!("hold response: {e}"))?;
    response
        .lines()
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|status| status.parse::<u16>().ok())
        .ok_or_else(|| "the hold did not answer with an HTTP status".to_string())
}

pub fn run_lifecycle(context: tauri::Context<tauri::Wry>) -> Result<(), String> {
    let expect = START.get().ok_or("startup marker was not written; refusing to start the check")?;
    let url = fixture_url(&expect.url)?;
    let deadline = expect.deadline;
    let built = tauri::Builder::default()
        .on_window_event(crate::hide_on_close)
        .manage(CheckState(std::sync::Mutex::new(CheckStateInner {
            receipt: Some(expect.receipt.clone()),
            ..Default::default()
        })))
        .manage(crate::WindowUrl(std::sync::Mutex::new(Some(expect.url.clone()))))
        .setup(move |app| {
            // Hidden and unfocused: a check never takes the screen.
            let built = WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url.clone()))
                .visible(false)
                .focused(false)
                .on_new_window(|_, _| tauri::webview::NewWindowResponse::<tauri::Wry>::Deny)
                .build();
            if let Err(error) = built {
                finish_lifecycle(
                    app.handle(),
                    serde_json::json!({"ok": false, "phase": "construction", "error": format!("window creation: {error}")}),
                );
            }
            Ok(())
        })
        .build(context);
    CONSTRUCTED.store(true, Ordering::SeqCst);
    let app = built.map_err(|error| format!("tauri build: {error}"))?;
    let handle = app.handle().clone();
    let timeout = handle.clone();
    std::thread::spawn(move || {
        std::thread::sleep(deadline.saturating_duration_since(std::time::Instant::now()));
        finish_lifecycle(
            &timeout,
            serde_json::json!({"ok": false, "phase": "lifecycle", "error": "the window lifecycle check timed out"}),
        );
    });
    let runner = handle.clone();
    std::thread::spawn(move || {
        std::thread::sleep(std::time::Duration::from_millis(1500)); // let the page load
        // WebKit construction can outlast the sleep; wait for the window, never guess.
        let mut waited = 0;
        while runner.get_webview_window("main").is_none() && waited < 200 {
            std::thread::sleep(std::time::Duration::from_millis(100));
            waited += 1;
        }
        let (visible_before, url_before) = window_state(&runner);
        let report = match runner.get_webview_window("main") {
            None => {
                let labels: Vec<String> = runner.webview_windows().keys().cloned().collect();
                serde_json::json!({"ok": false, "phase": "lifecycle", "error": "the window was never created", "windows": labels})
            }
            Some(window) => {
                let closed = window.close();
                std::thread::sleep(std::time::Duration::from_millis(800));
                let (visible_after_close, url_after_close) = window_state(&runner);
                let window_survived = url_after_close.is_some();
                let hid = window_survived && !visible_after_close;
                let hold = hold_request();
                // The app stays hidden while the check drives the show path, so the
                // test window can never paint on the owner's screen: a hidden app
                // orders its windows in without displaying them.
                #[cfg(target_os = "macos")]
                let _ = runner.hide();
                crate::run_action(&runner, "show-window");
                std::thread::sleep(std::time::Duration::from_millis(800));
                let (visible_reopened, url_reopened) = window_state(&runner);
                if let Some(window) = runner.get_webview_window("main") {
                    let _ = window.hide();
                }
                let ok = !visible_before
                    && closed.is_ok()
                    && hid
                    && visible_reopened
                    && url_reopened == url_before
                    && hold == Ok(200);
                serde_json::json!({
                    "ok": ok,
                    "phase": "lifecycle",
                    "lifecycle": {
                        "pid": std::process::id(),
                        "startedHidden": !visible_before,
                        "close": if closed.is_ok() { "requested" } else { "failed" },
                        "windowSurvivedClose": window_survived,
                        "hiddenAfterClose": !visible_after_close,
                        "reopenedVisible": visible_reopened,
                        "sameUrlAfterReopen": url_reopened == url_before,
                        "holdWhileHidden": match &hold { Ok(status) => serde_json::json!(status), Err(_) => serde_json::Value::Null },
                        "holdError": match &hold { Ok(_) => serde_json::Value::Null, Err(e) => serde_json::json!(e) },
                    },
                })
            }
        };
        finish_lifecycle(&runner, report);
    });
    app.run(|_, _| {});
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    const NONCE: &str = "feedfacecafebeef00112233445566aa";
    const URL: &str = "http://127.0.0.1:64800/";

    struct Fixture {
        root: PathBuf,
        home: PathBuf,
        downloads: PathBuf,
        library: PathBuf,
    }

    fn fixture() -> Fixture {
        // One directory per call: parallel tests used to delete each other's fixture.
        static NEXT: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
        let root = std::env::temp_dir().join(format!(
            "library-check-test-{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        ));
        let _ = std::fs::remove_dir_all(&root);
        let home = root.join("home");
        let downloads = root.join("downloads");
        let library = root.join("library");
        std::fs::create_dir_all(&home).unwrap();
        std::fs::create_dir_all(&downloads).unwrap();
        std::fs::create_dir_all(&library).unwrap();
        let home = home.canonicalize().unwrap();
        Fixture { root: root.canonicalize().unwrap(), home, downloads: downloads.canonicalize().unwrap(), library: library.canonicalize().unwrap() }
    }

    fn expect_for(fx: &Fixture) -> Expect {
        Expect {
            nonce: NONCE.into(),
            url: URL.into(),
            home: fx.home.clone(),
            work: fx.root.clone(),
            receipt: receipt_path(&fx.home),
            password: "x".into(),
            controls: CONTROLS,
            timeout_secs: DEFAULT_TIMEOUT_SECS,
            deadline: std::time::Instant::now() + std::time::Duration::from_secs(DEFAULT_TIMEOUT_SECS),
        }
    }

    fn complete_report(fx: &Fixture) -> serde_json::Value {
        let dir = fx.downloads.join("Sub Folder");
        std::fs::create_dir_all(&dir).unwrap();
        serde_json::json!({
            "nonce": NONCE,
            "fixtureBinding": {"localUrl": URL, "downloadPath": fx.downloads, "libraryPath": fx.library},
            "controls": CONTROLS,
            "chosen": CONTROLS,
            "typeable": CONTROLS,
            "clipboard": {"feedback": "Copied.", "state": "ok", "readbackOk": true, "readbackError": null},
            "folder": {"expected": dir, "chosen": dir, "exact": true, "typeable": true, "status": format!("Chose {}", dir.display())},
            "picker": format!("Chose {}", dir.display())
        })
    }

    #[test]
    fn check_dispatch_fails_closed_before_any_tauri_setup() {
        let main = include_str!("main.rs");
        // Only the fn main() body: the mod gate above it is a different cfg line.
        let body = main.split("fn main()").nth(1).expect("fn main body");
        let feature = body
            .split(r#"#[cfg(feature = "webview-check")]"#)
            .nth(1)
            .and_then(|s| s.split(r#"#[cfg(not(feature = "webview-check"))]"#).next())
            .unwrap_or("");
        let refuse = body
            .split(r#"#[cfg(not(feature = "webview-check"))]"#)
            .nth(1)
            .and_then(|s| s.split("tauri::Builder::default()").next())
            .unwrap_or("");
        // Opt-in branch: marker first, then the check entry behind the one app_context call.
        let start = feature.find("webview_check::start(").expect("start() dispatch");
        let run = feature.find("webview_check::run(").expect("run() dispatch");
        assert!(start < run, "start() must precede run()");
        assert!(
            feature.contains("webview_check::run(app_context())"),
            "the single app_context invocation sits behind the failure paths"
        );
        assert!(start < feature.find("webview_check::run(app_context())").unwrap());
        // Refusal branch: message and nonzero exit, no Tauri of any kind.
        assert!(refuse.contains("REFUSED"), "feature-free build must refuse loudly");
        assert!(refuse.contains("std::process::exit(1)"), "refusal must exit nonzero");
        assert!(!refuse.contains("app_context") && !refuse.contains("tauri::Builder"));
        // One check entry, one Tauri context symbol, one normal launch after the dispatch.
        assert_eq!(main.matches("webview_check::run(").count(), 1);
        assert_eq!(main.matches("generate_context!").count(), 1);
        assert_eq!(main.matches("tauri::Builder::default()").count(), 1);
        let dispatch = main.find("--webview-check").expect("flag check");
        assert!(dispatch < main.find("tauri::Builder::default()").unwrap());
    }

    #[test]
    fn failure_receipt_is_written_verbatim() {
        let root = std::env::temp_dir().join(format!("library-receipt-test-{}", std::process::id()));
        std::fs::create_dir_all(&root).unwrap();
        let path = root.join("native-webview-result.json");
        let report = serde_json::json!({"ok":false,"error":"fixture"});
        assert!(write_receipt(&path, &report).is_ok());
        // Round-trip through the same serializer: map order is not the contract,
        // the receipt's content plus its trailing newline is.
        assert_eq!(
            std::fs::read_to_string(&path).unwrap(),
            format!("{report}\n")
        );
        let parsed: serde_json::Value =
            serde_json::from_str(&std::fs::read_to_string(&path).unwrap()).unwrap();
        assert_eq!(parsed["ok"], serde_json::json!(false));
        assert_eq!(parsed["error"], "fixture");
        let _ = std::fs::remove_dir_all(root);
    }

    #[test]
    fn startup_marker_records_exact_identity() {
        let fx = fixture();
        let mut env_actual = serde_json::Map::new();
        for (var, name) in ISOLATED_ENV {
            env_actual.insert(var.to_string(), serde_json::Value::String(format!("/tmp/{name}")));
        }
        let marker = startup_marker(
            NONCE,
            Path::new("/bin/example"),
            &"a".repeat(64),
            env_actual,
            URL,
            &fx.home,
            1_700_000_000,
        );
        assert_eq!(marker["kind"], "native-webview-startup");
        assert_eq!(marker["runId"], NONCE);
        assert!(marker["pid"].as_u64().unwrap_or(0) > 0);
        assert_eq!(marker["exeSha256"].as_str().unwrap().len(), 64);
        assert!(marker["build"]["debugAssertions"].is_boolean());
        assert!(marker["build"]["features"]["webview-check"].as_bool().unwrap());
        for (var, _) in ISOLATED_ENV {
            assert!(marker["env"][var].is_string(), "marker must record {var}");
        }
        assert_eq!(marker["fixture"]["origin"], URL);
        assert_eq!(marker["fixture"]["home"], fx.home.display().to_string());
        let _ = std::fs::remove_dir_all(&fx.root);
    }

    #[test]
    fn watchdog_bound_comes_from_the_runner_env() {
        assert_eq!(parse_timeout("60"), Some(60));
        assert_eq!(parse_timeout("5"), Some(5));
        assert_eq!(parse_timeout("600"), Some(600));
        assert_eq!(parse_timeout("0"), None);
        assert_eq!(parse_timeout("601"), None);
        assert_eq!(parse_timeout("abc"), None);
        assert_eq!(parse_timeout(" 30 ".trim()), Some(30));
    }

    #[test]
    fn partial_reports_cannot_pass() {
        let fx = fixture();
        let expect = expect_for(&fx);
        let full = complete_report(&fx);
        assert!(webview_report_complete(&full, &expect), "complete report must pass");

        let outside = fx.root.parent().unwrap().join("elsewhere");
        std::fs::create_dir_all(&outside).unwrap();
        let mut cases: Vec<(&str, serde_json::Value)> = Vec::new();
        let mut v = full.clone(); v["nonce"] = serde_json::json!("another-run");
        cases.push(("run identity", v));
        let mut v = full.clone(); v["fixtureBinding"]["localUrl"] = serde_json::json!("http://127.0.0.1:9999/");
        cases.push(("fixture origin", v));
        let mut v = full.clone(); v["fixtureBinding"]["downloadPath"] = serde_json::json!(outside);
        cases.push(("download path outside work", v));
        let mut v = full.clone(); v["fixtureBinding"]["libraryPath"] = serde_json::json!("/tmp");
        cases.push(("library path outside work", v));
        let mut v = full.clone(); v["controls"] = serde_json::json!(8);
        cases.push(("control count", v));
        let mut v = full.clone(); v["chosen"] = serde_json::json!(8);
        cases.push(("chosen count", v));
        let mut v = full.clone(); v["clipboard"]["state"] = serde_json::json!("error");
        cases.push(("clipboard feedback only", v));
        let mut v = full.clone(); v["clipboard"]["readbackOk"] = serde_json::json!(false);
        cases.push(("clipboard without readback", v));
        let mut v = full.clone(); v["folder"]["exact"] = serde_json::json!(false);
        cases.push(("inexact folder", v));
        let mut v = full.clone(); v["folder"]["chosen"] = serde_json::json!(outside);
        cases.push(("folder outside work", v));
        let mut v = full.clone(); v["folder"]["typeable"] = serde_json::json!(false);
        cases.push(("folder field left read-only", v));
        let mut v = full.clone(); v["typeable"] = serde_json::json!(0);
        cases.push(("no typeable path field", v));
        let mut v = full.clone(); v["picker"] = serde_json::json!("");
        cases.push(("missing picker outcome", v));
        let mut v = full.clone(); v.as_object_mut().unwrap().remove("fixtureBinding");
        cases.push(("missing fixture binding", v));
        let mut v = full.clone(); v.as_object_mut().unwrap().remove("clipboard");
        cases.push(("missing clipboard evidence", v));

        for (name, report) in &cases {
            assert!(!webview_report_complete(report, &expect), "{name} must not pass");
        }
        let _ = std::fs::remove_dir_all(&fx.root);
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
        std::fs::write(home.join("settings.json"), b"fixture").unwrap();
        assert!(prepared_home(&home).is_ok());
        let _ = std::fs::remove_dir_all(root);
    }
}
