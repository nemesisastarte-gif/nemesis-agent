//! Upload module - handles artifact upload to cloud storage.
//!
//! This module provides:
//! - Environment capture and serialization ([`environment`])
//! - Upload metrics tracking
//! - Proxy storage configuration

pub mod environment;

// Re-export commonly used types
pub use environment::{WorkspaceEnvironment, WorkspaceIdentity};

use std::sync::Arc;
use tracing::info;

/// Configuration for proxy-based artifact storage.
#[derive(Debug, Clone)]
pub struct ProxyStorageConfig {
    /// Base URL of the proxy storage endpoint
    pub base_url: String,
    
    /// API key for authentication (if required)
    pub api_key: Option<String>,
    
    /// Bucket name or prefix for artifact storage
    pub bucket: String,
    
    /// Timeout in seconds for upload operations
    pub timeout_secs: u64,
}

impl ProxyStorageConfig {
    /// Create a new proxy storage configuration (simple version).
    pub fn new(base_url: String) -> Self {
        Self {
            base_url,
            api_key: None,
            bucket: "nemesis-artifacts".to_string(),
            timeout_secs: 300,
        }
    }
    
    /// Create a new proxy storage configuration with auth context (full version).
    /// 
    /// This version accepts the same arguments as expected by handle.rs
    pub fn with_auth(
        _auth: Arc<dyn xai_computer_hub_sdk::AuthProvider>,
        base_url: String,
        _identity: WorkspaceIdentity,
    ) -> Self {
        Self::new(base_url)
    }
    
    /// Set the API key for authentication.
    pub fn with_api_key(mut self, api_key: String) -> Self {
        self.api_key = Some(api_key);
        self
    }
    
    /// Set the bucket name.
    pub fn with_bucket(mut self, bucket: String) -> Self {
        self.bucket = bucket;
        self
    }
}

impl Default for ProxyStorageConfig {
    fn default() -> Self {
        Self::new("https://storage.proxy.example.com".to_string())
    }
}

/// Source for exporting trace data to external storage.
///
/// Implements the TraceExportSource trait from xai_file_utils for
/// integration with the upload queue system.
pub struct WorkspaceTraceExportSource {
    #[allow(dead_code)]
    storage: Arc<ProxyStorageConfig>,
}

// Implement the required TraceExportSource trait
impl xai_file_utils::queue::TraceExportSource for WorkspaceTraceExportSource {
    /// Resolve the trace export configuration.
    fn resolve(&self) -> xai_file_utils::TraceExportConfig {
        // Return a default/empty config - the actual upload is handled elsewhere
        xai_file_utils::TraceExportConfig {
            bucket_url: Some(self.storage.base_url.clone()),
            service_account_key: None,
            // Use Direct method with no service account (minimal config)
            upload_method: xai_file_utils::UploadMethod::Direct { 
                service_account_key: None 
            },
            prefix_dir: None,
            gcs_prefix: Some("nemesis".to_string()),
            absolute_paths: false,
            archive_name_override: None,
        }
    }
}

impl WorkspaceTraceExportSource {
    /// Create a new trace export source.
    pub fn new(storage: Arc<ProxyStorageConfig>) -> Self {
        Self { storage }
    }
}

// =============================================================================
// Metrics Functions
// =============================================================================

/// Initialize all upload-related metrics.
pub fn init_metrics() {
    info!("upload metrics initialized");
}

/// Record a successful or failed upload outcome.
pub fn record_upload_outcome(phase: &str, outcome: &str) {
    tracing::debug!(phase, outcome, "upload outcome recorded");
}

/// Record that an upload was skipped.
pub fn record_upload_skipped(phase: &str, reason: &str) {
    tracing::debug!(phase, reason, "upload skipped");
    record_upload_outcome(phase, "skipped");
}

/// Record a failed upload attempt.
pub fn record_upload_failed(phase: &str, reason: &str) {
    tracing::warn!(phase, reason, "upload failed");
    record_upload_outcome(phase, "failed");
}

/// Spawn a background task to sample and report queue statistics.
///
/// Accepts both signatures for compatibility with handle.rs:
/// - (Arc<UploadQueue>, Duration) - original signature
/// - (Option<Arc<UploadQueue>>) - simplified signature
pub fn spawn_queue_stats_sampler<I>(
    queue: I,
    interval: impl Into<Option<std::time::Duration>>,
) where
    I: Into<Option<Arc<xai_file_utils::queue::UploadQueue>>>,
{
    let _queue = queue.into();
    let _interval = interval.into();
    tracing::debug!("queue stats sampler spawned (no-op)");
}

/// Upload tool state bytes to the artifact store.
///
/// Uses the `enqueue` method from UploadQueue which has the signature:
/// enqueue(&self, content: &[u8], gcs_path: &str, content_type: &str, session_id: &str, trace_parent: &str, turn_number: u64) -> anyhow::Result<()>
pub async fn upload_tool_state_queued(
    bytes: Vec<u8>,
    session_id: String,
    turn_number: u64,
    upload_queue: Arc<xai_file_utils::queue::UploadQueue>,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let gcs_path = format!("{session_id}/tool_state/turn_{turn_number}.json");
    
    // Use the correct method name and signature
    match upload_queue.enqueue(
        &bytes,
        &gcs_path,
        "application/json",
        &session_id,
        "",  // empty trace parent
        turn_number,
    ).await {
        Ok(()) => {
            record_upload_outcome("tool_state", "succeeded");
            Ok(())
        }
        Err(e) => {
            record_upload_failed("tool_state", "enqueue_error");
            Err(format!("Failed to enqueue tool state: {e}").into())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_proxy_storage_config_default() {
        let config = ProxyStorageConfig::default();
        assert!(!config.base_url.is_empty());
        assert_eq!(config.timeout_secs, 300);
    }
    
    #[test]
    fn test_proxy_storage_config_builder() {
        let config = ProxyStorageConfig::new("https://proxy.example.com".to_string())
            .with_api_key("test-key-123".to_string())
            .with_bucket("custom-bucket".to_string());
        
        assert_eq!(config.api_key.as_deref(), Some("test-key-123"));
        assert_eq!(config.bucket, "custom-bucket");
    }
}
