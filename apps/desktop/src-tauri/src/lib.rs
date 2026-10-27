mod engine;

use engine::EngineBridge;
use serde_json::Value;
use std::sync::Arc;
use tauri::{Manager, State};

#[tauri::command]
async fn engine_request(
    bridge: State<'_, Arc<EngineBridge>>,
    method: String,
    params: Option<Value>,
    timeout_ms: Option<u64>,
) -> Result<Value, Value> {
    bridge
        .request(
            method,
            params.unwrap_or(Value::Null),
            timeout_ms.unwrap_or(30_000),
        )
        .await
        .map_err(|e| e.to_value())
}

#[tauri::command]
async fn engine_status(bridge: State<'_, Arc<EngineBridge>>) -> Result<Value, Value> {
    Ok(bridge.status().await)
}

#[tauri::command]
async fn engine_restart(bridge: State<'_, Arc<EngineBridge>>) -> Result<Value, Value> {
    let b = bridge.inner().clone();
    b.restart().await.map_err(|e| e.to_value())?;
    Ok(b.status().await)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();

    tauri::Builder::default()
        .setup(|app| {
            let bridge = Arc::new(EngineBridge::new(app.handle().clone()));
            app.manage(bridge.clone());
            tauri::async_runtime::spawn(async move {
                if let Err(err) = bridge.start().await {
                    log::error!("failed to start engine: {err}");
                }
            });
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            engine_request,
            engine_status,
            engine_restart
        ])
        .run(tauri::generate_context!())
        .expect("error while running Relay");
}
