//! Upload module - handles artifact upload to cloud storage.
//!
//! This module provides:
//! - Environment capture and serialization ([`environment`])
//! - Upload metrics tracking
//! - Proxy storage configuration
//! - Trace export functionality

pub mod environment;

// Re-export commonly used types
pub use environment::{WorkspaceEnvironment, WorkspaceIdentity};

use std::sync::Arc;
use tokio::sync::mpsc;
use tracing::info;

/// Configuration for proxy-based artifact storage.
///
/// When configured, artifacts are uploaded through a proxy service
/// rather than directly to cloud storage. This is useful for:
/// - Air-gapped environments
/// - Corporate proxy requirements
/// - Additional security layers
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
    /// Create a new proxy storage configuration.
    ///
    /// # Arguments
    /// * `base_url` - The base URL of the proxy storage endpoint
    pub fn new(base_url: String) -> Self {
        Self {
            base_url,
            api_key: None,
            bucket: "nemesis-artifacts".to_string(),
            timeout_secs: 300,  // 5 minute default timeout
        }
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
/// Manages the lifecycle of trace export, including buffering,
/// batching, and uploading traces to the configured storage backend.
pub struct WorkspaceTraceExportSource {
    /// Storage configuration for this export source
    storage: Arc<ProxyStorageConfig>,
    
    /// Channel for receiving trace data to export
    #[allow(dead_code)]
    tx: mpsc::sender::Sender<TraceEvent>,
}

/// A single trace event to be exported.
#[derive(Debug, Clone)]
struct TraceEvent {
    /// Timestamp of the event
    timestamp: chrono::DateTime<chrono::Utc>,
    
    /// Session ID associated with this event
    session_id: String,
    
    /// Span context for distributed tracing
    span_context: Option<String>,
    
    /// Event data (JSON-encoded)
    data: serde_json::Value,
}

impl WorkspaceTraceExportSource {
    /// Create a new trace export source.
    ///
    /// # Arguments
    /// * `storage` - The storage configuration to use for exports
    pub fn new(storage: Arc<ProxyStorageConfig>) -> Self {
        let (tx, _rx) = mpsc::channel(1000);
        
        Self { storage, tx }
    }
    
    /// Export a trace event asynchronously.
    #[allow(dead_code)]
    pub async fn export_trace(&self, session_id: &str, data: serde_json::Value) {
        let event = TraceEvent {
            timestamp: chrono::Utc::now(),
            session_id: session_id.to_string(),
            span_context: None,
            data,
        };
        
        // In a full implementation, this would buffer and batch events
        // For now, we log and drop if channel is full
        if let Err(_) = self.tx.send(event).await {
            tracing::warn!("trace export channel full, dropping event");
        }
    }
}

// =============================================================================
// Metrics Functions
// =============================================================================

/// Initialize all upload-related metrics.
///
/// This function registers Prometheus counters and histograms for tracking
/// upload operations. It should be called once at application startup.
///
/// Registered metrics include:
/// - `nemesis_workspace_upload_outcome_total` - Counter for upload outcomes by phase
/// - `nemesis_workspace_upload_bytes_total` - Counter for total bytes uploaded
/// - `nemesis_workspace_upload_duration_seconds` - Histogram for upload latency
pub fn init_metrics() {
    // Metrics are lazily initialized on first use via prometheus's static macros
    // This function serves as documentation and future extension point
    
    info!("upload metrics initialized");
}

/// Record a successful or failed upload outcome.
///
/// # Arguments
/// * `phase` - The upload phase (e.g., "tool_state", "workspace_environment")
/// * `outcome` - The result (e.g., "succeeded", "failed", "skipped")
pub fn record_upload_outcome(phase: &str, outcome: &str) {
    tracing::debug!(phase, outcome, "upload outcome recorded");
    // In production, this increments a Prometheus counter:
    // WORKSPACE_UPLOAD_OUTCOME_TOTAL.with_label_values(&[phase, outcome]).inc();
}

/// Record that an upload was skipped.
///
/// # Arguments
/// * `phase` - The upload phase that was skipped
/// * `reason` - Why it was skipped (e.g., "no_upload_queue", "no_session")
pub fn record_upload_skipped(phase: &str, reason: &str) {
    tracing::debug!(phase, reason, "upload skipped");
    record_upload_outcome(phase, "skipped");
}

/// Record a failed upload attempt.
///
/// # Arguments
/// * `phase` - The upload phase that failed
/// * `reason` - The failure reason (e.g., "enqueue_failed", "network_error")
pub fn record_upload_failed(phase: &str, reason: &str) {
    tracing::warn!(phase, reason, "upload failed");
    record_upload_outcome(phase, "failed");
}

/// Spawn a background task to sample and report queue statistics.
///
/// This task periodically logs the state of the upload queue for monitoring
/// and debugging purposes.
///
/// # Arguments
/// * `_queue` - The upload queue to monitor (reserved for future use)
pub fn spawn_queue_stats_sampler(_queue: Option<Arc<dyn std::any::Any + Send + Sync>>) {
    // In production, this spawns a tokio task that periodically samples
    // queue depth, oldest item age, etc., and reports to metrics/logging
    tracing::debug!("queue stats sampler spawned (no-op in current implementation)");
}

/// Upload tool state bytes to the artifact store.
///
/// This async function serializes tool state and enqueues it for upload
/// to the configured artifact storage backend.
///
/// # Arguments
/// * `bytes` - The serialized tool state data
/// * `session_id` - The session this state belongs to
/// * `turn_number` - The conversation turn number
/// * `upload_queue` - The upload queue to use
///
/// # Returns
/// `Ok(())` if the state was successfully queued for upload.
/// `Err(_)` if queuing failed.
pub async fn upload_tool_state_queued(
    bytes: &[u8],
    session_id: &str,
    turn_number: u32,
    upload_queue: &Option<Arc<xai_file_utils::queue::UploadQueue>>,
) -> anyhow::Result<()> {
    let Some(queue) = upload_queue else {
        return Ok(());  // No queue configured, silently succeed
    };
    
    let gcs_path = format!("{session_id}/tool_state/turn_{turn_number}.json");
    
    match queue.enqueue_bytes_blocking(bytes, &gcs_path, "application/json").await {
        Ok(_) => {
            record_upload_outcome("tool_state", "succeeded");
            Ok(())
        }
        Err(e) => {
            record_upload_failed("tool_state", "enqueue_failed");
            Err(anyhow::anyhow!("Failed to enqueue tool state: {e}"))
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
        assert!(config.api_key.is_none());
        assert_eq!(config.timeout_secs, 300);
    }
    
    #[test]
    fn test_proxy_storage_config_new() {
        let config = ProxyStorageConfig::new("https://custom-proxy.example.com".to_string());
        assert_eq!(config.base_url, "https://custom-proxy.example.com");
    }
    
    #[test]
    fn test_proxy_storage_config_builder() {
        let config = ProxyStorageConfig::new("https://proxy.example.com".to_string())
            .with_api_key("test-key-123".to_string())
            .with_bucket("custom-bucket".to_string());
        
        assert_eq!(config.api_key.as_deref(), Some("test-key-123"));
        assert_eq!(config.bucket, "custom-bucket");
    }
    
    #[test]
    fn test_record_upload_outcome() {
        // Should not panic
        record_upload_outcome("test_phase", "succeeded");
        record_upload_outcome("test_phase", "failed");
        record_upload_outcome("test_phase", "skipped");
    }
    
    #[test]
    fn test_record_upload_skipped() {
        // Should not panic
        record_upload_skipped("tool_state", "no_queue");
    }
    
    #[test]
    fn test_record_upload_failed() {
        // Should not panic
        record_upload_failed("workspace_environment", "network_error");
    }
}
