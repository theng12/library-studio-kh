// Desktop shell: starts the bundled server, then shows the address it prints in a window.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::{Manager, RunEvent, WebviewUrl, WebviewWindowBuilder};

#[cfg(feature = "webview-check")]
mod webview_check;

fn app_context() -> tauri::Context<tauri::Wry> { tauri::generate_context!() }

struct Server(Mutex<Option<Child>>);

// The URL the main window shows, kept so a closed window can be rebuilt.
struct WindowUrl(Mutex<Option<String>>);

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
// closed, from the application menu or the Dock.
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

// A second copy or an orphaned helper holding the host ports must never turn into a
// panic: say so plainly and exit, so macOS shows a clean quit instead of an abort.
const HOST_PORTS: [u16; 2] = [7881, 7882];

fn another_instance_running() -> bool {
    HOST_PORTS.iter().any(|port| {
        std::net::TcpStream::connect_timeout(
            &std::net::SocketAddr::from(([127, 0, 0, 1], *port)),
            std::time::Duration::from_millis(200),
        )
        .is_ok()
    })
}

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
    let stderr = child.stderr.take();
    let mut lines = BufReader::new(stdout).lines();
    while let Some(Ok(line)) = lines.next() {
        if let Some(url) = line.strip_prefix("LISTENING ") {
            let url = url.to_string();
            std::thread::spawn(move || lines.for_each(drop)); // keep the pipe drained
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

// Quitting stops the background host too, so no Library Studio KH Helper keeps
// running behind an app that is gone. Closing the window keeps macOS's app alive.
fn stop_host() {
    let Ok(exe) = server_path() else { return };
    match Command::new(exe)
        .args(["-host-control", "stop"])
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
    // Fail closed first: --webview-check never reaches app_context, Tauri setup,
    // HOME, host, window, menu or server work unless this build opted in with
    // --features webview-check. A flag it cannot run is never a normal launch.
    if std::env::args().any(|arg| arg == "--webview-check") {
        #[cfg(feature = "webview-check")]
        {
            // start() isolates the environment and writes the startup marker
            // before app_context()/WebKit can exist.
            if let Err(error) = webview_check::start() {
                eprintln!("native-webview-check: FAIL ({error})");
                std::process::exit(1);
            }
            if let Err(error) = webview_check::run(app_context()) {
                eprintln!("native-webview-check: FAIL ({error})");
                std::process::exit(1);
            }
            return;
        }
        #[cfg(not(feature = "webview-check"))]
        {
            eprintln!(
                "native-webview-check: REFUSED — this build has no webview-check feature; \
                 ordinary and release builds never run --webview-check \
                 (rebuild with --features webview-check for verification)"
            );
            std::process::exit(1);
        }
    }
    tauri::Builder::default()
        .setup(|app| {
            build_menu(app)?;
            // A second bundle or an orphaned helper holding the host ports is a
            // plain message and a clean exit, never a panic inside an Apple Event.
            if another_instance_running() {
                report_and_exit("Another Library Studio KH is already running on this Mac. Quit it, then open this one again.");
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
        .on_menu_event(|app, event| {
            if event.id() == "use-local-host" {
                use_local_host(app);
            } else if event.id() == "restart" {
                stop_server(app);
                app.restart();
            } else if event.id() == "show-window" {
                show_main(app);
            } else if event.id() == "quit" {
                app.exit(0);
            }
        })
        .build(app_context())
        .expect("Library Studio KH could not start")
        .run(|app, event| {
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
