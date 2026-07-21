//! Upload module for NEMESIS shell.
//!
//! This module provides artifact upload functionality:
//! - GCS (Google Cloud Storage) integration
//! - Session trace metadata upload
//! - Artifact tracking and manifests
//! - Turn-level trace collection

pub mod gcs;
pub mod manifest;
pub mod trace;
pub mod turn;

// Re-exports for convenience
pub use gcs::SESSION_TRACES_BUCKET;
pub use manifest::{ArtifactTracker, ArtifactUploadContext};
pub use turn::SyntheticTurnTraceRequest;
pub use trace::{SessionMetadataType, upload_session_metadata};
