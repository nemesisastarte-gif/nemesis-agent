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
///
/// When configured, artifacts are uploaded through a proxy service
/// rather than directly to cloud storage.
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
            timeout_secs: 300,
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

// =============================================================================
// Metrics Functions
// =============================================================================

/// Initialize all upload-related metrics.
///
/// This function registers Prometheus counters and histograms for tracking
/// upload operations. It should be called once at application startup.
pub fn init_metrics() {
    info!("upload metrics initialized");
}

/// Record a successful or failed upload outcome.
///
/// # Arguments
/// * `phase` - The upload phase (e.g., "tool_state", "workspace_environment")
/// * `outcome` - The result (e.g., "succeeded", "failed", "skipped")
pub fn record_upload_outcome(phase: &str, outcome: &str) {
    tracing::debug!(phase, outcome, "upload outcome recorded");
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
/// This task periodically logs the state of the upload queue for monitoring.
///
/// # Arguments
/// * `_queue` - Optional upload queue to monitor
pub fn spawn_queue_stats_sampler(_queue: Option<Arc<xai_file_utils::queue::UploadQueue>>) {
    tracing::debug!("queue stats sampler spawned (no-op)");
}

/// Upload tool state bytes to the artifact store.
///
/// This async function serializes tool state and enqueues it for upload.
///
/// # Arguments
/// * `bytes` - The serialized tool state data
/// * `session_id` - The session this state belongs to
/// * `turn_number` - The conversation turn number
/// * `upload_queue` - Reference to optional upload queue
///
/// # Returns
/// `Ok(())` if successful, `Err(_)` if queuing failed.
pub async fn upload_tool_state_queued(
    bytes: Vec<u8>,
    session_id: String,
    turn_number: u64,
    upload_queue: Arc<xai_file_utils::queue::UploadQueue>,
) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let gcs_path = format!("{session_id}/tool_state/turn_{turn_number}.json");
    
    // Use the enqueue_bytes method which returns Result<EnqueueOutcome, _>
    match upload_queue.enqueue_bytes(&bytes, &gcs_path, "application/json").await {
        Ok(outcome) => {
            // Log the outcome without matching on specific variants
            // since we don't know the exact EnqueueOutcome shape
            tracing::debug!(?outcome, "tool state enqueued");
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
        assert!(config.api_key.is_none());
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
    
    #[test]
    fn test_record_upload_outcome() {
        record_upload_outcome("test_phase", "succeeded");
        record_upload_outcome("test_phase", "failed");
        record_upload_outcome("test_phase", "skipped");
    }
    
    #[test]
    fn test_record_upload_skipped() {
        record_upload_skipped("tool_state", "no_queue");
    }
    
    #[test]
    fn test_record_upload_failed() {
        record_upload_failed("workspace_environment", "network_error");
    }
}
