use serde_json::{json, Value};
use std::collections::HashMap;
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::atomic::{AtomicBool, AtomicU32, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;
use tauri::{AppHandle, Emitter, Manager};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::process::{Child, ChildStdin, Command};
use tokio::sync::{oneshot, Mutex, RwLock};
use uuid::Uuid;

/// Errors surfaced to the webview as structured JSON so the UI can branch on
/// `code` instead of parsing strings.
#[derive(Debug, Clone)]
pub struct EngineError {
    pub code: String,
    pub message: String,
    pub retryable: bool,
    pub details: Value,
}

impl EngineError {
    fn new(code: &str, message: impl Into<String>, retryable: bool) -> Self {
        Self {
            code: code.into(),
            message: message.into(),
            retryable,
            details: Value::Null,
        }
    }
    fn not_ready() -> Self {
        Self::new("engine_unavailable", "The Relay engine is not running", true)
    }
    pub fn to_value(&self) -> Value {
        json!({
            "code": self.code,
            "message": self.message,
            "retryable": self.retryable,
            "details": self.details,
        })
    }
}

impl std::fmt::Display for EngineError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.code, self.message)
    }
}

type PendingMap = HashMap<String, oneshot::Sender<Result<Value, EngineError>>>;

/// Methods the webview may not invoke; the host owns the engine lifecycle.
const HOST_ONLY_METHODS: &[&str] = &["shutdown"];
const MAX_TIMEOUT_MS: u64 = 10 * 60 * 1000;

pub struct EngineBridge {
    app: AppHandle,
    ready: AtomicBool,
    starting: Mutex<()>,
    protocol_version: AtomicU32,
    generation: AtomicU64,
    restarts: AtomicU32,
    stdin: Mutex<Option<ChildStdin>>,
    child: Mutex<Option<Child>>,
    pending: Arc<Mutex<PendingMap>>,
    data_dir: RwLock<PathBuf>,
    hello: RwLock<Value>,
    last_error: RwLock<Option<String>>,
}

impl EngineBridge {
    pub fn new(app: AppHandle) -> Self {
        let data_dir = app
            .path()
            .app_data_dir()
            .unwrap_or_else(|_| PathBuf::from(".relay-data"));
        Self {
            app,
            ready: AtomicBool::new(false),
            starting: Mutex::new(()),
            protocol_version: AtomicU32::new(0),
            generation: AtomicU64::new(0),
            restarts: AtomicU32::new(0),
            stdin: Mutex::new(None),
            child: Mutex::new(None),
            pending: Arc::new(Mutex::new(HashMap::new())),
            data_dir: RwLock::new(data_dir),
            hello: RwLock::new(Value::Null),
            last_error: RwLock::new(None),
        }
    }

    pub fn is_ready(&self) -> bool {
        self.ready.load(Ordering::SeqCst)
    }

    pub async fn status(&self) -> Value {
        json!({
            "ready": self.is_ready(),
            "protocolVersion": self.protocol_version.load(Ordering::SeqCst),
            "restarts": self.restarts.load(Ordering::SeqCst),
            "hello": self.hello.read().await.clone(),
            "lastError": self.last_error.read().await.clone(),
        })
    }

    fn emit(&self, event: &str, payload: Value) {
        let _ = self
            .app
            .emit("engine-event", json!({"event": event, "payload": payload}));
    }

    pub async fn start(self: &Arc<Self>) -> Result<(), EngineError> {
        let _guard = self.starting.lock().await;
        if self.is_ready() {
            return Ok(());
        }
        let result = self.spawn_and_handshake().await;
        if let Err(err) = &result {
            *self.last_error.write().await = Some(err.message.clone());
            self.emit("engine.error", json!({"message": err.message}));
        } else {
            *self.last_error.write().await = None;
        }
        result
    }

    fn start_boxed(
        this: Arc<Self>,
    ) -> std::pin::Pin<Box<dyn std::future::Future<Output = Result<(), EngineError>> + Send>>
    {
        Box::pin(async move { this.start().await })
    }

    pub async fn restart(self: &Arc<Self>) -> Result<(), EngineError> {
        self.stop().await;
        self.restarts.fetch_add(1, Ordering::SeqCst);
        self.start().await
    }

    async fn stop(&self) {
        self.ready.store(false, Ordering::SeqCst);
        // Closing stdin asks the engine to shut down gracefully.
        self.stdin.lock().await.take();
        if let Some(mut child) = self.child.lock().await.take() {
            if tokio::time::timeout(Duration::from_secs(5), child.wait())
                .await
                .is_err()
            {
                let _ = child.kill().await;
            }
        }
        fail_all(&self.pending, "engine stopped").await;
    }

    async fn spawn_and_handshake(self: &Arc<Self>) -> Result<(), EngineError> {
        let bin = resolve_engine_binary(&self.app)?;
        let data_dir = self.data_dir.read().await.clone();
        std::fs::create_dir_all(&data_dir).map_err(|e| {
            EngineError::new("internal", format!("cannot create data directory: {e}"), false)
        })?;

        let mut cmd = Command::new(&bin);
        cmd.arg("--data-dir")
            .arg(&data_dir)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .kill_on_drop(true);

        let mut child = cmd.spawn().map_err(|e| {
            EngineError::new("internal", format!("failed to start engine: {e}"), true)
        })?;
        let stdout = child.stdout.take();
        let stderr = child.stderr.take();
        let stdin = child.stdin.take();
        let (Some(stdout), Some(stderr), Some(stdin)) = (stdout, stderr, stdin) else {
            return Err(EngineError::new("internal", "engine pipes unavailable", false));
        };

        let generation = self.generation.fetch_add(1, Ordering::SeqCst) + 1;
        *self.stdin.lock().await = Some(stdin);
        *self.child.lock().await = Some(child);

        let (ready_tx, ready_rx) = oneshot::channel::<()>();
        let this = self.clone();
        tauri::async_runtime::spawn(async move {
            let mut ready_tx = Some(ready_tx);
            let mut lines = BufReader::new(stdout).lines();
            while let Ok(Some(line)) = lines.next_line().await {
                match handle_stdout_line(&this.app, &this.pending, &line).await {
                    Ok(true) => {
                        if let Some(tx) = ready_tx.take() {
                            let _ = tx.send(());
                        }
                    }
                    Ok(false) => {}
                    Err(err) => log::warn!("engine stdout: {err}"),
                }
            }
            this.on_exit(generation).await;
        });

        tauri::async_runtime::spawn(async move {
            let mut lines = BufReader::new(stderr).lines();
            while let Ok(Some(line)) = lines.next_line().await {
                // The engine redacts secrets before logging.
                log::info!("engine: {line}");
            }
        });

        if tokio::time::timeout(Duration::from_secs(15), ready_rx)
            .await
            .map(|r| r.is_err())
            .unwrap_or(true)
        {
            self.stop().await;
            return Err(EngineError::new(
                "engine_unavailable",
                "engine did not become ready (see application log)",
                true,
            ));
        }

        let hello = self
            .send(
                "hello".into(),
                json!({"protocolVersion": 1, "client": "relay-desktop", "clientVersion": env!("CARGO_PKG_VERSION")}),
                10_000,
            )
            .await?;
        if let Some(v) = hello.get("protocolVersion").and_then(|v| v.as_u64()) {
            self.protocol_version.store(v as u32, Ordering::SeqCst);
        }
        *self.hello.write().await = hello.clone();
        self.ready.store(true, Ordering::SeqCst);
        self.emit("engine.ready", hello);
        Ok(())
    }

    async fn on_exit(self: &Arc<Self>, generation: u64) {
        if self.generation.load(Ordering::SeqCst) != generation {
            return; // an older process from before a restart
        }
        let was_ready = self.ready.swap(false, Ordering::SeqCst);
        self.stdin.lock().await.take();
        let code = match self.child.lock().await.take() {
            Some(mut c) => c.wait().await.ok().and_then(|s| s.code()),
            None => None,
        };
        fail_all(&self.pending, "engine exited").await;
        let msg = match code {
            Some(c) => format!("engine process exited (code {c})"),
            None => "engine process exited".to_string(),
        };
        *self.last_error.write().await = Some(msg.clone());
        self.emit("engine.exited", json!({"message": msg, "code": code}));

        // Unexpected crash: restart automatically a bounded number of times.
        if was_ready && self.restarts.load(Ordering::SeqCst) < 3 {
            let this = self.clone();
            tauri::async_runtime::spawn(async move {
                let n = this.restarts.fetch_add(1, Ordering::SeqCst) + 1;
                tokio::time::sleep(Duration::from_millis(500 * u64::from(n))).await;
                if let Err(err) = Self::start_boxed(this).await {
                    log::error!("engine auto-restart failed: {err}");
                }
            });
        }
    }

    /// Request from the webview.
    pub async fn request(
        &self,
        method: String,
        params: Value,
        timeout_ms: u64,
    ) -> Result<Value, EngineError> {
        if HOST_ONLY_METHODS.contains(&method.as_str()) {
            return Err(EngineError::new(
                "authorization_error",
                "method not available to the UI",
                false,
            ));
        }
        if !self.is_ready() {
            return Err(EngineError::not_ready());
        }
        self.send(method, params, timeout_ms.clamp(100, MAX_TIMEOUT_MS))
            .await
    }

    async fn send(
        &self,
        method: String,
        params: Value,
        timeout_ms: u64,
    ) -> Result<Value, EngineError> {
        let id = Uuid::new_v4().to_string();
        let (tx, rx) = oneshot::channel();
        self.pending.lock().await.insert(id.clone(), tx);

        let frame = json!({
            "type": "request",
            "id": id,
            "method": method,
            "params": params,
            "timeoutMs": timeout_ms,
        });
        let write = async {
            let mut guard = self.stdin.lock().await;
            let stdin = guard.as_mut().ok_or_else(EngineError::not_ready)?;
            let mut line = serde_json::to_vec(&frame)
                .map_err(|e| EngineError::new("internal", e.to_string(), false))?;
            line.push(b'\n');
            stdin
                .write_all(&line)
                .await
                .map_err(|_| EngineError::not_ready())?;
            stdin.flush().await.map_err(|_| EngineError::not_ready())
        };
        if let Err(e) = write.await {
            self.pending.lock().await.remove(&id);
            return Err(e);
        }

        match tokio::time::timeout(Duration::from_millis(timeout_ms + 1_000), rx).await {
            Ok(Ok(res)) => res,
            Ok(Err(_)) => Err(EngineError::not_ready()),
            Err(_) => {
                self.pending.lock().await.remove(&id);
                // Tell the engine to stop work nobody is waiting for.
                let cancel = json!({"type":"request","id":Uuid::new_v4().to_string(),"method":"cancel","params":{"targetId": id}});
                if let Some(stdin) = self.stdin.lock().await.as_mut() {
                    if let Ok(mut line) = serde_json::to_vec(&cancel) {
                        line.push(b'\n');
                        let _ = stdin.write_all(&line).await;
                    }
                }
                Err(EngineError::new("timeout", "engine did not respond in time", true))
            }
        }
    }
}

async fn fail_all(pending: &Arc<Mutex<PendingMap>>, reason: &str) {
    let mut map = pending.lock().await;
    for (_, tx) in map.drain() {
        let _ = tx.send(Err(EngineError::new("engine_unavailable", reason, true)));
    }
}

/// Returns Ok(true) when the line is the engine.ready event.
async fn handle_stdout_line(
    app: &AppHandle,
    pending: &Arc<Mutex<PendingMap>>,
    line: &str,
) -> Result<bool, String> {
    let value: Value = serde_json::from_str(line).map_err(|e| e.to_string())?;
    match value.get("type").and_then(|v| v.as_str()).unwrap_or("") {
        "response" => {
            let id = value
                .get("id")
                .and_then(|v| v.as_str())
                .ok_or_else(|| "response missing id".to_string())?
                .to_string();
            let ok = value.get("ok").and_then(|v| v.as_bool()).unwrap_or(false);
            let result = if ok {
                Ok(value.get("result").cloned().unwrap_or(Value::Null))
            } else {
                let err = value.get("error").cloned().unwrap_or(json!({}));
                let s = |k: &str, d: &str| {
                    err.get(k)
                        .and_then(|v| v.as_str())
                        .unwrap_or(d)
                        .to_string()
                };
                Err(EngineError {
                    code: s("code", "internal"),
                    message: s("message", "engine error"),
                    retryable: err
                        .get("retryable")
                        .and_then(|v| v.as_bool())
                        .unwrap_or(false),
                    details: err.get("details").cloned().unwrap_or(Value::Null),
                })
            };
            if let Some(tx) = pending.lock().await.remove(&id) {
                let _ = tx.send(result);
            }
            Ok(false)
        }
        "event" => {
            // engine.ready is re-emitted by the host after the hello handshake.
            if value.get("event").and_then(|v| v.as_str()) == Some("engine.ready") {
                return Ok(true);
            }
            let _ = app.emit("engine-event", value);
            Ok(false)
        }
        _ => Ok(false),
    }
}

fn resolve_engine_binary(_app: &AppHandle) -> Result<PathBuf, EngineError> {
    let triple = current_target_triple();
    let names = [
        format!("relay-engine-{triple}{}", std::env::consts::EXE_SUFFIX),
        format!("relay-engine{}", std::env::consts::EXE_SUFFIX),
    ];
    let mut dirs: Vec<PathBuf> = Vec::new();
    // Bundled sidecars (tauri `externalBin`) are placed next to the main executable.
    if let Some(dir) = std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(|p| p.to_path_buf()))
    {
        dirs.push(dir);
    }
    #[cfg(debug_assertions)]
    dirs.push(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("binaries"));

    for dir in &dirs {
        for name in &names {
            let c = dir.join(name);
            if c.is_file() {
                return Ok(c);
            }
        }
    }
    Err(EngineError::new(
        "engine_unavailable",
        "relay-engine binary not found; run `make engine` first",
        false,
    ))
}

fn current_target_triple() -> &'static str {
    #[cfg(all(target_os = "macos", target_arch = "aarch64"))]
    {
        "aarch64-apple-darwin"
    }
    #[cfg(all(target_os = "macos", target_arch = "x86_64"))]
    {
        "x86_64-apple-darwin"
    }
    #[cfg(all(target_os = "linux", target_arch = "x86_64"))]
    {
        "x86_64-unknown-linux-gnu"
    }
    #[cfg(all(target_os = "linux", target_arch = "aarch64"))]
    {
        "aarch64-unknown-linux-gnu"
    }
    #[cfg(all(target_os = "windows", target_arch = "x86_64"))]
    {
        "x86_64-pc-windows-msvc"
    }
    #[cfg(not(any(
        all(target_os = "macos", target_arch = "aarch64"),
        all(target_os = "macos", target_arch = "x86_64"),
        all(target_os = "linux", target_arch = "x86_64"),
        all(target_os = "linux", target_arch = "aarch64"),
        all(target_os = "windows", target_arch = "x86_64"),
    )))]
    {
        "unknown"
    }
}
