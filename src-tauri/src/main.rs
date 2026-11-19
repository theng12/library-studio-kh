// Desktop shell: starts the bundled server, then shows the address it prints in a window.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::{Manager, RunEvent, WebviewUrl, WebviewWindowBuilder, WindowEvent};

#[cfg(feature = "webview-check")]
mod webview_check;

fn app_context() -> tauri::Context<tauri::Wry> { tauri::generate_context!() }

struct Server(Mutex<Option<Child>>);

// The URL the main window shows, kept so a closed window can be rebuilt.
pub(crate) struct WindowUrl(pub(crate) Mutex<Option<String>>);

fn open_main_window(app: &tauri::AppHandle, url: &str) -> Result<tauri::WebviewWindow, String> {
    let target = url
        .parse()
        .map_err(|e| format!("the server address could not be read: {e}"))?;
    WebviewWindowBuilder::new(app, "main", WebviewUrl::External(target))
        .title("Library Studio KH")
        .inner_size(1040.0, 780.0)
        .min_inner_size(420.0, 480.0)
        // The Support link opens the one allow-listed page in the
        // default browser, never as an in-app window.
        .on_new_window(|url, _| {
            if url.as_str() == SUPPORT_URL {
                open_support();
            }
            tauri::webview::NewWindowResponse::<tauri::Wry>::Deny
        })
        .build()
        .map_err(|e| e.to_string())
}

// A window click must never do nothing: show the window, or rebuild it if it was
// closed, from the tray, the application menu or the Dock.
fn show_main(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
        return;
    }
    let url = app
        .state::<WindowUrl>()
        .0
        .lock()
        .ok()
        .and_then(|saved| saved.clone());
    if let Some(url) = url {
        if let Err(e) = open_main_window(app, &url) {
            eprintln!("The Library window could not open: {e}");
        }
    }
}

// Closing the window hides it instead of quitting: downloads, imports, the server
// and the update hold keep running. Only Quit stops Library.
pub(crate) fn hide_on_close(window: &tauri::Window, event: &WindowEvent) {
    if let WindowEvent::CloseRequested { api, .. } = event {
        api.prevent_close();
        let _ = window.hide();
    }
}

// One action table for the app menu and the menu bar item.
pub(crate) fn run_action(app: &tauri::AppHandle, id: &str) {
    match id {
        "use-local-host" => use_local_host(app),
        "restart" => {
            stop_server(app);
            app.restart();
        }
        "show-window" => show_main(app),
        "quit" => app.exit(0),
        _ => {}
    }
}

// The menu bar item, so the window can always come back with the app out of sight.
fn build_tray(app: &tauri::App) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "show-window", "Show Library Studio KH", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit Library Studio KH", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &quit])?;
    let mut tray = tauri::tray::TrayIconBuilder::with_id("library-tray")
        .tooltip("Library Studio KH")
        .menu(&menu)
        .on_menu_event(|app, event| run_action(app, event.id().as_ref()));
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    tray.build(app)?;
    Ok(())
}

// The only external page this app opens. Kept in step with the link in the
// Support page; anything else is denied.
const SUPPORT_URL: &str = "https://nowpayments.io/donation/studiokh";

fn open_support() {
    #[cfg(target_os = "macos")]
    let opened = Command::new("open").arg(SUPPORT_URL).spawn();
    #[cfg(target_os = "linux")]
    let opened = Command::new("xdg-open").arg(SUPPORT_URL).spawn();
    #[cfg(target_os = "windows")]
    let opened = Command::new("cmd")
        .args(["/C", "start", "", SUPPORT_URL])
        .spawn();
    if let Err(e) = opened {
        eprintln!("Could not open the support page: {e}");
    }
}

fn server_path() -> Result<PathBuf, String> {
    // Tauri installs the sidecar next to the app executable, without the target suffix.
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let name = if cfg!(windows) {
        "library-studio-server.exe"
    } else {
        "library-studio-server"
    };
    Ok(exe.with_file_name(name))
}

// A server that cannot start must never turn into a panic: the server says why in
// plain words and this shows it, so macOS gets a clean quit instead of an abort.
//
// This used to probe the background host's ports first and refuse as "another
// Library Studio KH is already running" — but a running background host IS this
// app's own service, which the server attaches to, so that probe refused to open
// the app on every Mac with the login job installed (an install over it left the
// app closed). A second real copy is refused by the server's configuration-home
// lock instead, which prints its own plain message.
fn report_and_exit(reason: &str) -> ! {
    eprintln!("Library Studio KH did not open: {reason}");
    if let Ok(dir) = std::env::var("HOME") {
        let path = PathBuf::from(dir)
            .join("Library")
            .join("Logs")
            .join("Library Studio KH launch error.txt");
        if let Some(parent) = path.parent() {
            let _ = std::fs::create_dir_all(parent);
        }
        let _ = std::fs::write(
            &path,
            format!("Library Studio KH did not open.\n\n{reason}\n"),
        );
    }
    std::process::exit(2);
}

fn start_server() -> Result<(Child, String), String> {
    let mut cmd = Command::new(server_path()?);
    cmd.arg("-with-shell")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x0800_0000); // CREATE_NO_WINDOW: no console window behind the app
    }
    let mut child = cmd
        .spawn()
        .map_err(|e| format!("could not start the server: {e}"))?;
    let stdout = match child.stdout.take() {
        Some(out) => out,
        None => {
            let _ = child.kill();
            return Err("the server did not report its address".into());
        }
    };
    let mut stderr = child.stderr.take();
    let mut lines = BufReader::new(stdout).lines();
    while let Some(Ok(line)) = lines.next() {
        if let Some(url) = line.strip_prefix("LISTENING ") {
            let url = url.to_string();
            std::thread::spawn(move || lines.for_each(drop)); // keep the pipe drained
            // Dropping stderr here kills the server: Go turns a write to a pipe
            // with no reader into SIGPIPE, so the first log line after launch ends
            // it and the window is left pointing at a dead address. Keep reading.
            if let Some(mut pipe) = stderr.take() {
                std::thread::spawn(move || {
                    let _ = std::io::copy(&mut pipe, &mut std::io::sink());
                });
            }
            return Ok((child, url));
        }
        if let Some(reason) = line.strip_prefix("BLOCKED ") {
            let reason = reason.trim().to_string();
            let _ = child.kill();
            return Err(reason);
        }
    }
    let _ = child.kill();
    let detail = stderr
        .and_then(|mut pipe| {
            let mut text = String::new();
            use std::io::Read;
            pipe.read_to_string(&mut text).ok().map(|_| text)
        })
        .map(|text| text.trim().to_string())
        .filter(|text| !text.is_empty());
    Err(detail.unwrap_or_else(|| "the server stopped before it was ready".into()))
}

// Quitting stops the background host process, so no Library Studio KH Helper keeps
// running behind an app that is gone. It keeps the login job loaded though: an
// installer that stops this app must find the job "loaded, no pid" and unload it
// itself. Unloading it here (the old "stop") leaves the job absent, which the
// updater reads as "did not stop safely" and retries forever (issues row
// 20261120-139). Closing the window only hides it.
fn stop_host() {
    let Ok(exe) = server_path() else { return };
    match Command::new(exe)
        .args(["-host-control", "halt"])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status()
    {
        Ok(status) if status.success() => {}
        Ok(status) => eprintln!("The background host did not stop cleanly (exit {status})."),
        Err(e) => eprintln!("Could not stop the background host: {e}"),
    }
}

fn stop_server(app: &tauri::AppHandle) {
    // This is only the desktop helper, never a background host service.
    if let Ok(mut slot) = app.state::<Server>().0.lock() {
        if let Some(mut c) = slot.take() {
            let _ = c.kill();
            let _ = c.wait();
        }
    }
}

fn build_menu(app: &tauri::App) -> tauri::Result<()> {
    let use_local = MenuItem::with_id(
        app,
        "use-local-host",
        "Use this Mac as host",
        true,
        None::<&str>,
    )?;
    let restart = MenuItem::with_id(app, "restart", "Restart Library", true, None::<&str>)?;
    let quit = MenuItem::with_id(
        app,
        "quit",
        "Quit Library Studio KH",
        true,
        Some("CmdOrCtrl+Q"),
    )?;
    let show = MenuItem::with_id(
        app,
        "show-window",
        "Show Library Studio KH",
        true,
        Some("CmdOrCtrl+0"),
    )?;
    #[cfg(target_os = "macos")]
    let app_menu = Submenu::with_items(
        app,
        "Library Studio KH",
        true,
        &[
            &PredefinedMenuItem::about(app, None, None)?,
            &PredefinedMenuItem::separator(app)?,
            &show,
            &PredefinedMenuItem::separator(app)?,
            &use_local,
            &restart,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::services(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::hide(app, None)?,
            &PredefinedMenuItem::hide_others(app, None)?,
            &PredefinedMenuItem::show_all(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &quit,
        ],
    )?;
    #[cfg(not(target_os = "macos"))]
    let app_menu = Submenu::with_items(
        app,
        "Library Studio KH",
        true,
        &[
            &show,
            &PredefinedMenuItem::separator(app)?,
            &use_local,
            &restart,
            &PredefinedMenuItem::separator(app)?,
            &quit,
        ],
    )?;
    // Predefined items target the focused responder, so editing works inside the
    // web content without routing keystrokes through the app.
    let edit_menu = Submenu::with_items(
        app,
        "Edit",
        true,
        &[
            &PredefinedMenuItem::undo(app, None)?,
            &PredefinedMenuItem::redo(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::cut(app, None)?,
            &PredefinedMenuItem::copy(app, None)?,
            &PredefinedMenuItem::paste(app, None)?,
            &PredefinedMenuItem::select_all(app, None)?,
        ],
    )?;
    let window_menu = Submenu::with_id_and_items(
        app,
        tauri::menu::WINDOW_SUBMENU_ID,
        "Window",
        true,
        &[
            &show,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::minimize(app, None)?,
            &PredefinedMenuItem::maximize(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::close_window(app, None)?,
        ],
    )?;
    app.set_menu(Menu::with_items(
        app,
        &[&app_menu, &edit_menu, &window_menu],
    )?)?;
    Ok(())
}

// Switching back is a local native action: the remote webpage cannot clear this
// desktop's saved client host through the server API.
fn use_local_host(app: &tauri::AppHandle) {
    let exe = match server_path() {
        Ok(path) => path,
        Err(e) => {
            eprintln!("Could not use this Mac as host: {e}");
            return;
        }
    };
    let status = Command::new(exe)
        .arg("-client-host")
        .arg("clear")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();
    match status {
        Ok(status) if status.success() => {
            stop_server(app);
            app.restart();
        }
        Ok(status) => eprintln!("Could not clear the saved host (exit {status})."),
        Err(e) => eprintln!("Could not clear the saved host: {e}"),
    }
}

fn main() {
    // Fail closed first: --webview-check and --window-lifecycle-check never reach
    // app_context, Tauri setup, HOME, host, window, menu or server work unless this
    // build opted in with --features webview-check. A flag it cannot run is never a
    // normal launch.
    if let Some(flag) = std::env::args()
        .find(|arg| arg == "--webview-check" || arg == "--window-lifecycle-check")
    {
        let lifecycle = flag == "--window-lifecycle-check";
        #[cfg(feature = "webview-check")]
        {
            // start() isolates the environment and writes the startup marker
            // before app_context()/WebKit can exist.
            if let Err(error) = webview_check::start() {
                eprintln!("native-webview-check: FAIL ({error})");
                std::process::exit(1);
            }
            let checked = if lifecycle {
                webview_check::run_lifecycle(app_context())
            } else {
                webview_check::run(app_context())
            };
            if let Err(error) = checked {
                eprintln!("native-webview-check: FAIL ({error})");
                std::process::exit(1);
            }
            return;
        }
        #[cfg(not(feature = "webview-check"))]
        {
            eprintln!(
                "native-webview-check: REFUSED — this build has no webview-check feature; \
                 ordinary and release builds never run --webview-check or \
                 --window-lifecycle-check \
                 (rebuild with --features webview-check for verification)"
            );
            std::process::exit(1);
        }
    }
    tauri::Builder::default()
        .on_window_event(hide_on_close)
        .setup(|app| {
            build_menu(app)?;
            // The menu bar item is a convenience: a platform without one still opens.
            if let Err(e) = build_tray(app) {
                eprintln!("The menu bar item could not open: {e}");
            }
            let (child, url) = match start_server() {
                Ok(started) => started,
                Err(reason) => report_and_exit(&reason),
            };
            app.manage(Server(Mutex::new(Some(child))));
            app.manage(WindowUrl(Mutex::new(Some(url.clone()))));
            if let Err(e) = open_main_window(app.handle(), &url) {
                report_and_exit(&format!("the Library window could not open: {e}"));
            }
            Ok(())
        })
        .on_menu_event(|app, event| run_action(app, event.id().as_ref()))
        .build(app_context())
        .expect("Library Studio KH could not start")
        .run(|app, event| {
            // No window left is not a quit either: only Quit, Cmd+Q or Restart exits.
            if let RunEvent::ExitRequested { code, api, .. } = &event {
                if code.is_none() {
                    api.prevent_exit();
                }
            }
            #[cfg(target_os = "macos")]
            if let RunEvent::Reopen { has_visible_windows, .. } = &event {
                // Clicking the Dock icon with no window open brings the window back.
                if !has_visible_windows {
                    show_main(app);
                }
            }
            if let RunEvent::Exit = event {
                // Mac Cmd+Q, AppleScript quit and the menu item all land here. No
                // failure may unwind through the Apple Event that asked us to quit.
                let _ = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                    stop_host();
                    stop_server(app);
                }));
            }
        });
}
