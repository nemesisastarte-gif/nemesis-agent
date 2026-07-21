//! Artifact manifest tracking for upload coordination.

use std::path::PathBuf;
use std::sync::Arc;
use tokio::sync::mpsc;

/// Tracks artifacts generated during a session for batch upload.
///
/// `ArtifactTracker` maintains a record of all artifacts (files, traces, etc.)
/// produced during a session turn, enabling efficient batch upload and
/// deduplication.
#[derive(Debug, Clone)]
pub struct ArtifactTracker {
    /// Session identifier this tracker belongs to
    pub session_id: String,
    
    /// Current turn number
    pub turn_number: u64,
    
    /// Base directory for artifact storage
    pub artifact_dir: PathBuf,
}

impl ArtifactTracker {
    /// Create a new artifact tracker for the given session and turn.
    pub fn new(session_id: String, turn_number: u64, base_dir: PathBuf) -> Self {
        Self {
            session_id,
            turn_number,
            artifact_dir: base_dir.join("artifacts").join(&session_id),
        }
    }
    
    /// Record an artifact for later upload.
    #[allow(unused_variables)]
    pub fn record_artifact(&self, artifact_type: &str, path: &PathBuf) {
        tracing::debug!(
            session_id = %self.session_id,
            turn = self.turn_number,
            artifact_type,
            path = %path.display(),
            "artifact recorded"
        );
    }
    
    /// Get the artifact directory for this turn.
    pub fn turn_dir(&self) -> PathBuf {
        self.artifact_dir.join(format!("turn_{}", self.turn_number))
    }
}

impl Default for ArtifactTracker {
    fn default() -> Self {
        Self::new(
            "default-session".to_string(),
            0,
            std::env::temp_dir().join("nemesis-artifacts"),
        )
    }
}

/// Context for managing artifact uploads during a session.
///
/// Contains all state needed to coordinate uploads across multiple turns,
/// including the upload queue, tracker, and configuration.
#[derive(Debug, Clone)]
pub struct ArtifactUploadContext {
    /// The artifact tracker for recording produced artifacts
    pub tracker: ArtifactTracker,
    
    /// Whether uploads are enabled for this context
    pub uploads_enabled: bool,
}

impl ArtifactUploadContext {
    /// Create a new upload context.
    pub fn new(tracker: ArtifactTracker, uploads_enabled: bool) -> Self {
        Self {
            tracker,
            uploads_enabled,
        }
    }
    
    /// Create a disabled context (no uploads will be performed).
    pub fn disabled(session_id: String) -> Self {
        Self::new(
            ArtifactTracker::new(session_id, 0, std::env::temp_dir()),
            false,
        )
    }
}
