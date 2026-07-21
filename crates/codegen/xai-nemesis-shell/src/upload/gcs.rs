//! Shell-side adapter that threads the live `AuthManager` through to the
//! `StorageClient` constructed inside `xai_file_utils::gcs::*` helpers.
//!
//! This is a simplified version of the original gcs.rs module adapted for
//! NEMESIS standalone builds where some internal types may not be available.

use std::sync::Arc;
use xai_file_utils::{TraceExportConfig, UploadMethod};

/// Owned wrapper that pairs a `TraceExportConfig` with an optional auth manager.
/// 
/// In the full build, this wraps an `Arc<AuthManager>` for refresh-aware credentials.
/// In this simplified version, it just holds the config.
#[derive(Clone)]
pub struct TraceExportConfigWithAuth {
    inner: TraceExportConfig,
    #[allow(dead_code)]
    auth_manager: Option<Arc<dyn std::any::Any + Send + Sync>>,
}

impl TraceExportConfigWithAuth {
    pub fn new(inner: TraceExportConfig, auth_manager: Option<Arc<dyn std::any::Any + Send + Sync>>) -> Self {
        Self { inner, auth_manager }
    }
    
    pub fn inner(&self) -> &TraceExportConfig {
        &self.inner
    }
}

impl std::ops::Deref for TraceExportConfigWithAuth {
    type Target = TraceExportConfig;
    
    fn deref(&self) -> &Self::Target {
        &self.inner
    }
}

/// Convenience trait for wrapping a `TraceExportConfig` at upload call sites.
/// 
/// Pattern:
/// ```ignore
/// xai_file_utils::gcs::upload_bytes(
///     &gcs_config.with_auth(Some(auth_manager.clone())),
///     ...,
/// ).await
/// ```
pub trait WithAuth {
    fn with_auth(&self, auth_manager: Option<Arc<dyn std::any::Any + Send + Sync>>) -> TraceExportConfigWithAuth;
}

impl WithAuth for TraceExportConfig {
    fn with_auth(&self, auth_manager: Option<Arc<dyn std::any::Any + Send + Sync>>) -> TraceExportConfigWithAuth {
        TraceExportConfigWithAuth::new(self.clone(), auth_manager)
    }
}

/// Default GCS bucket for session trace uploads.
/// 
/// Uses `option_env!` to allow compile-time override. Returns `None` if not configured,
/// which disables trace uploads until a bucket is configured at runtime.
pub const SESSION_TRACES_BUCKET: Option<&str> = option_env!("GROK_SESSION_TRACES_BUCKET_DEFAULT");

/// Build the GCS console browse URL for the per-turn unified log.
///
/// The log is already uploaded by `complete_prompt_trace` at
/// `{session_id}/turn_{N}/unified_log.jsonl`. This just computes
/// the URL so feedback Slack messages can link to it.
///
/// * `bucket_url` - The resolved trace bucket (`gs://…`) so the link tracks runtime overrides
/// * `session_id` - The session identifier  
/// * `turn_number` - The turn number
///
/// Returns `None` when no GCS bucket is configured.
#[allow(unused_variables)]
pub fn unified_log_url(
    bucket_url: Option<&str>,
    session_id: &str,
    turn_number: i64,
) -> Option<String> {
    let bucket = match bucket_url {
        Some(url) => url.strip_prefix("gs://")?.trim_end_matches('/'),
        None => SESSION_TRACES_BUCKET?,
    };
    Some(format!(
        "https://console.cloud.google.com/storage/browser/_details/{bucket}/{session_id}/turn_{turn_number}/unified_log.jsonl"
    ))
}
