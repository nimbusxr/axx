// The depot desk in Tauri (../../README.md): the page in ../../web, what it
// keeps in the app's data and config folders, and the Depot menu.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::fs;
use std::path::PathBuf;

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tauri::menu::{Menu, MenuItem, PredefinedMenuItem, Submenu};
use tauri::{AppHandle, Emitter, Manager, State, Wry};

#[derive(Serialize, Deserialize)]
struct Arrival {
    reference: String,
    level: String,
    fragile: bool,
}

#[derive(Serialize)]
struct Kept {
    arrivals: Vec<Arrival>,
    level: Option<String>,
}

struct CloseDay(MenuItem<Wry>);

fn arrivals_file(app: &AppHandle) -> Result<PathBuf, String> {
    Ok(app.path().app_data_dir().map_err(|e| e.to_string())?.join("arrivals.json"))
}

fn settings_file(app: &AppHandle) -> Result<PathBuf, String> {
    Ok(app.path().app_config_dir().map_err(|e| e.to_string())?.join("settings.json"))
}

fn write(path: PathBuf, value: &impl Serialize) -> Result<(), String> {
    fs::create_dir_all(path.parent().unwrap()).map_err(|e| e.to_string())?;
    fs::write(path, serde_json::to_vec(value).unwrap()).map_err(|e| e.to_string())
}

#[tauri::command]
fn load(app: AppHandle) -> Result<Kept, String> {
    let arrivals = fs::read(arrivals_file(&app)?)
        .ok()
        .and_then(|b| serde_json::from_slice(&b).ok())
        .unwrap_or_default();
    let level = fs::read(settings_file(&app)?)
        .ok()
        .and_then(|b| serde_json::from_slice::<Value>(&b).ok())
        .and_then(|v| v["serviceLevel"].as_str().map(String::from));
    Ok(Kept { arrivals, level })
}

#[tauri::command]
fn save_arrivals(app: AppHandle, arrivals: Vec<Arrival>) -> Result<(), String> {
    write(arrivals_file(&app)?, &arrivals)
}

#[tauri::command]
fn save_level(app: AppHandle, level: String) -> Result<(), String> {
    write(settings_file(&app)?, &json!({ "serviceLevel": level }))
}

#[tauri::command]
fn set_close_day_enabled(item: State<CloseDay>, enabled: bool) -> Result<(), String> {
    item.0.set_enabled(enabled).map_err(|e| e.to_string())
}

fn main() {
    tauri::Builder::default()
        .setup(|app| {
            let close = MenuItem::with_id(app, "close-day", "Close day", false, None::<&str>)?;
            let depot = Submenu::with_items(app, "Depot", true, &[&close])?;
            let menu = if cfg!(target_os = "macos") {
                let quit = PredefinedMenuItem::quit(app, None)?;
                let desk = Submenu::with_items(app, "Depot desk", true, &[&quit])?;
                Menu::with_items(app, &[&desk, &depot])?
            } else {
                Menu::with_items(app, &[&depot])?
            };
            app.set_menu(menu)?;
            app.on_menu_event(|app, event| {
                if event.id() == "close-day" {
                    let _ = app.emit("close-day", ());
                }
            });
            app.manage(CloseDay(close));
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![load, save_arrivals, save_level, set_close_day_enabled])
        .run(tauri::generate_context!())
        .expect("the depot desk could not run");
}
