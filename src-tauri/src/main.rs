// Desktop shell: starts the bundled server, then shows the address it prints in a window.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::{Manager, RunEvent, WebviewUrl, WebviewWindowBuilder};

struct Server(Mutex<Option<Child>>);

fn server_path() -> Result<PathBuf, String> {
    // Tauri installs the sidecar next to the app executable, without the target suffix.
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let name = if cfg!(windows) { "library-studio-server.exe" } else { "library-studio-server" };
    Ok(exe.with_file_name(name))
}

fn start_server() -> Result<(Child, String), String> {
    let mut cmd = Command::new(server_path()?);
    cmd.arg("-with-shell").stdin(Stdio::piped()).stdout(Stdio::piped());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x0800_0000); // CREATE_NO_WINDOW: no console window behind the app
    }
    let mut child = cmd.spawn().map_err(|e| format!("could not start the server: {e}"))?;
    let mut lines = BufReader::new(child.stdout.take().unwrap()).lines();
    while let Some(Ok(line)) = lines.next() {
        if let Some(url) = line.strip_prefix("LISTENING ") {
            let url = url.to_string();
            std::thread::spawn(move || lines.for_each(drop)); // keep the pipe drained
            return Ok((child, url));
        }
    }
    let _ = child.kill();
    Err("the server stopped before it was ready".into())
}

fn stop_server(app: &tauri::AppHandle) {
    // This is only the desktop helper, never a background host service.
    if let Some(mut c) = app.state::<Server>().0.lock().unwrap().take() {
        let _ = c.kill();
        let _ = c.wait();
    }
}

fn build_menu(app: &tauri::App) -> tauri::Result<()> {
    let use_local = MenuItem::with_id(app, "use-local-host", "Use this Mac as host", true, None::<&str>)?;
    let restart = MenuItem::with_id(app, "restart", "Restart Library", true, None::<&str>)?;
    #[cfg(target_os = "macos")]
    let app_menu = Submenu::with_items(
        app,
        "Library Studio KH",
        true,
        &[
            &PredefinedMenuItem::about(app, None, None)?,
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
            &PredefinedMenuItem::quit(app, None)?,
        ],
    )?;
    #[cfg(not(target_os = "macos"))]
    let app_menu = Submenu::with_items(
        app,
        "Library Studio KH",
        true,
        &[
            &use_local,
            &restart,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::quit(app, None)?,
        ],
    )?;
    app.set_menu(Menu::with_items(app, &[&app_menu])?)?;
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
    tauri::Builder::default()
        .setup(|app| {
            build_menu(app)?;
            let (child, url) = start_server()?;
            app.manage(Server(Mutex::new(Some(child))));
            WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url.parse()?))
                .title("Library Studio KH")
                .inner_size(1040.0, 780.0)
                .min_inner_size(420.0, 480.0)
                .build()?;
            Ok(())
        })
        .on_menu_event(|app, event| {
            if event.id() == "use-local-host" {
                use_local_host(app);
            } else if event.id() == "restart" {
                stop_server(app);
                app.restart();
            }
        })
        .build(tauri::generate_context!())
        .expect("Library Studio KH could not start")
        .run(|app, event| {
            if let RunEvent::Exit = event {
                stop_server(app);
            }
        });
}
