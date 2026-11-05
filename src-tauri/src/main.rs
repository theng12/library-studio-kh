// Desktop shell: starts the bundled server, then shows the address it prints in a window.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::io::{BufRead, BufReader};
use std::process::{Child, Command, Stdio};
use std::sync::Mutex;
use tauri::{Manager, RunEvent, WebviewUrl, WebviewWindowBuilder};

struct Server(Mutex<Option<Child>>);

fn start_server() -> Result<(Child, String), String> {
    // Tauri installs the sidecar next to the app executable, without the target suffix.
    let exe = std::env::current_exe().map_err(|e| e.to_string())?;
    let name = if cfg!(windows) { "library-studio-server.exe" } else { "library-studio-server" };
    let mut cmd = Command::new(exe.with_file_name(name));
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

fn main() {
    tauri::Builder::default()
        .setup(|app| {
            let (child, url) = start_server()?;
            app.manage(Server(Mutex::new(Some(child))));
            WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url.parse()?))
                .title("Library Studio KH")
                .inner_size(1040.0, 780.0)
                .min_inner_size(420.0, 480.0)
                .build()?;
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("Library Studio KH could not start")
        .run(|app, event| {
            if let RunEvent::Exit = event {
                if let Some(mut c) = app.state::<Server>().0.lock().unwrap().take() {
                    let _ = c.kill();
                    let _ = c.wait();
                }
            }
        });
}
