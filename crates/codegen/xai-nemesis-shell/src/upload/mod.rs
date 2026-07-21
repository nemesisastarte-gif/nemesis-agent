//! Upload module for NEMESIS shell.
//!
//! This module provides artifact upload functionality:
//! - GCS (Google Cloud Storage) integration via `gcs`
//! - Session trace metadata upload via `trace`
//! - Artifact tracking and manifests via `manifest`
//! - Turn-level trace collection via `turn`

pub mod gcs;
pub mod manifest;
pub mod trace;
pub mod turn;

// Re-exports for convenience
// Note: SESSION_TRACES_BUCKET is Option<&str> per the original
pub use gcs::{SESSION_TRACES_BUCKET, TraceExportConfigWithAuth, WithAuth, unified_log_url};
pub use manifest::{
    ArtifactTracker, ArtifactTrackerInner, ArtifactUploadContext, ArtifactStatus,
    UploadManifest, build_manifest, new_artifact_tracker, record_artifact, skip_artifact,
};
pub use turn::SyntheticTurnTraceRequest;
pub use trace::{SessionMetadata, SessionMetadataType, upload_session_metadata};
