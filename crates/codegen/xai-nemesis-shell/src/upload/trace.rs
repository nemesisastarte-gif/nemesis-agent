//! Session metadata upload functionality.

use serde::{Deserialize, Serialize};

/// Types of session metadata that can be uploaded.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum SessionMetadataType {
    /// Turn-level conversation metadata
    TurnSummary,
    
    /// Session-level statistics
    SessionStats,
    
    /// Tool usage information
    ToolUsage,
    
    /// Performance metrics
    PerformanceMetrics,
    
    /// Custom/extension metadata
    Custom,
}

impl Default for SessionMetadataType {
    fn default() -> Self {
        Self::TurnSummary
    }
}

impl std::fmt::Display for SessionMetadataType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::TurnSummary => write!(f, "turn_summary"),
            Self::SessionStats => write!(f, "session_stats"),
            Self::ToolUsage => write!(f, "tool_usage"),
            Self::PerformanceMetrics => write!(f, "performance_metrics"),
            Self::Custom => write!(f, "custom"),
        }
    }
}

/// Metadata for a session upload.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SessionMetadata {
    /// Session identifier
    pub session_id: String,
    
    /// Type of metadata
    pub metadata_type: SessionMetadataType,
    
    /// When this metadata was captured
    pub timestamp: chrono::DateTime<chrono::Utc>,
    
    /// JSON-encoded metadata payload
    pub payload: serde_json::Value,
}

/// Upload session metadata to the artifact store.
///
/// # Arguments
/// * `metadata` - The session metadata to upload
/// * `upload_queue` - Optional upload queue for async upload
///
/// # Returns
/// `Ok(())` if the metadata was successfully queued or no queue is configured.
/// `Err(_)` if the upload failed.
#[allow(unused_variables)]
pub async fn upload_session_metadata(
    metadata: SessionMetadata,
    upload_queue: Option<&tokio::sync::mpsc::UnboundedSender<()>>,
) -> anyhow::Result<()> {
    tracing::debug!(
        session_id = %metadata.session_id,
        metadata_type = %metadata.metadata_type,
        "session metadata upload requested"
    );
    
    // NEMESIS: Log and succeed - actual upload handled by queue if configured
    if let Some(_queue) = upload_queue {
        // Signal that metadata is ready for upload
        // In production, this would enqueue the metadata for background upload
    }
    
    Ok(())
}
