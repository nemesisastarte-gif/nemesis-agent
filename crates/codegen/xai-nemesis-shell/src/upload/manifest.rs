//! Upload manifest: authoritative "turn upload is done" signal.
//!
//! This module provides artifact tracking for turn-level uploads,
//! recording which artifacts succeeded, failed, or were skipped.

use chrono::{DateTime, Utc};
use std::collections::HashMap;
use std::sync::Arc;

pub(crate) const MANIFEST_SCHEMA_VERSION: u32 = 3;

/// Status of an individual artifact upload.
#[derive(Debug, serde::Serialize, Clone, Copy)]
#[serde(rename_all = "snake_case")]
pub enum ArtifactStatus {
    /// Upload completed successfully
    Succeeded,
    /// Upload failed
    Failed,
    /// Upload was skipped (not needed)
    Skipped,
    /// Artifact was enqueued for background upload
    Enqueued,
}

/// Method used to upload artifacts.
#[derive(serde::Serialize, Clone, Copy)]
#[serde(rename_all = "snake_case")]
pub enum ManifestUploadMethod {
    /// Upload via proxy
    Proxy,
    /// Direct upload to GCS
    Direct,
    /// Upload to S3-compatible storage
    S3,
}

impl ManifestUploadMethod {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Proxy => "proxy",
            Self::Direct => "direct",
            Self::S3 => "s3",
        }
    }
}

/// Details about a failed upload.
#[derive(Debug, serde::Serialize, Clone)]
pub struct FailureDetail {
    pub reason: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

/// Complete upload manifest for a turn.
#[derive(serde::Serialize)]
pub struct UploadManifest {
    pub schema_version: u32,
    pub fully_uploaded: bool,
    pub completed_at: DateTime<Utc>,
    pub upload_method: ManifestUploadMethod,
    pub artifacts: HashMap<String, ArtifactStatus>,
    #[serde(skip_serializing_if = "HashMap::is_empty")]
    pub failure_details: HashMap<String, FailureDetail>,
    #[serde(skip_serializing_if = "HashMap::is_empty")]
    pub skip_details: HashMap<String, String>,
}

impl UploadManifest {
    pub fn error(upload_method: ManifestUploadMethod) -> Self {
        Self {
            schema_version: MANIFEST_SCHEMA_VERSION,
            fully_uploaded: false,
            completed_at: Utc::now(),
            upload_method,
            artifacts: HashMap::new(),
            failure_details: HashMap::new(),
            skip_details: HashMap::new(),
        }
    }
}

/// Internal state of the artifact tracker.
#[derive(Debug, Default)]
pub struct ArtifactTrackerInner {
    pub statuses: HashMap<String, ArtifactStatus>,
    pub failures: HashMap<String, FailureDetail>,
    pub skips: HashMap<String, String>,
}

/// Thread-safe artifact tracker for recording upload outcomes.
///
/// Uses `parking_lot::Mutex` for low-overhead synchronization.
pub type ArtifactTracker = Arc<parking_lot::Mutex<ArtifactTrackerInner>>;

/// Create a new artifact tracker instance.
pub fn new_artifact_tracker() -> ArtifactTracker {
    Arc::new(parking_lot::Mutex::new(ArtifactTrackerInner::default()))
}

/// Result of an individual artifact upload operation.
pub enum ArtifactResult<'a> {
    /// Upload succeeded
    Succeeded,
    /// Handed to the async upload pipeline
    Enqueued,
    /// Upload failed
    Failed {
        reason: &'a str,
        error: Option<&'a str>,
    },
}

/// Record an artifact upload result in the tracker.
pub fn record_artifact(
    tracker: &ArtifactTracker,
    filename: &str,
    result: ArtifactResult<'_>,
) {
    match result {
        ArtifactResult::Succeeded => {
            tracker
                .lock()
                .statuses
                .insert(filename.to_owned(), ArtifactStatus::Succeeded);
        }
        ArtifactResult::Enqueued => {
            tracker
                .lock()
                .statuses
                .insert(filename.to_owned(), ArtifactStatus::Enqueued);
        }
        ArtifactResult::Failed { reason, error } => {
            let key = filename.to_owned();
            let mut inner = tracker.lock();
            inner.statuses.insert(key.clone(), ArtifactStatus::Failed);
            inner.failures.insert(
                key,
                FailureDetail {
                    reason: reason.to_owned(),
                    error: error.map(truncate),
                },
            );
        }
    }
}

/// Record that an artifact was skipped.
pub fn skip_artifact(tracker: &ArtifactTracker, filename: &str, reason: &str) {
    let key = filename.to_owned();
    let mut inner = tracker.lock();
    inner.statuses.insert(key.clone(), ArtifactStatus::Skipped);
    inner.skips.insert(key, reason.to_owned());
}

fn truncate(s: &str) -> &str {
    match s.char_indices().nth(512) {
        Some((idx, _)) => &s[..idx],
        None => s,
    }
}

/// Build a complete manifest from the tracker state.
pub fn build_manifest(
    tracker: &ArtifactTracker,
    upload_method: ManifestUploadMethod,
) -> UploadManifest {
    let inner = tracker.lock();
    let artifacts = inner.statuses.clone();
    let failure_details: HashMap<String, FailureDetail> = inner
        .failures
        .iter()
        .filter(|(k, _)| matches!(artifacts.get(k.as_str()), Some(ArtifactStatus::Failed)))
        .map(|(k, v)| (k.clone(), v.clone()))
        .collect();
    let skip_details: HashMap<String, String> = inner
        .skips
        .iter()
        .filter(|(k, _)| matches!(artifacts.get(k.as_str()), Some(ArtifactStatus::Skipped)))
        .map(|(k, v)| (k.clone(), v.clone()))
        .collect();
    let fully_uploaded = !artifacts
        .values()
        .any(|s| matches!(s, ArtifactStatus::Failed));
    
    UploadManifest {
        schema_version: MANIFEST_SCHEMA_VERSION,
        fully_uploaded,
        completed_at: Utc::now(),
        upload_method,
        artifacts,
        failure_details,
        skip_details,
    }
}

/// Context for managing artifact uploads during a session.
#[derive(Debug, Clone)]
pub struct ArtifactUploadContext {
    /// GCS configuration for uploads
    #[allow(dead_code)]
    pub gcs_config: xai_file_utils::TraceExportConfig,
    
    /// The artifact tracker for recording produced artifacts  
    pub artifact_tracker: ArtifactTracker,
}
