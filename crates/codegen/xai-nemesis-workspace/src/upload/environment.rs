//! Environment capture and workspace identity for upload artifacts.
//!
//! This module provides:
//! - [`WorkspaceEnvironment`] - Captures and serializes workspace environment state
//! - [`WorkspaceIdentity`] - Identifies the workspace owner/user

use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

/// Identity of the workspace owner/user.
///
/// Used to tag uploaded artifacts with ownership information
/// for audit trails and access control.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct WorkspaceIdentity {
    /// Unique user identifier (e.g., "user-123" or "account:xxx")
    pub user_id: String,
    /// Human-readable display name
    pub display_name: Option<String>,
    /// Email address (optional)
    pub email: Option<String>,
}

impl WorkspaceIdentity {
    /// Create a new workspace identity.
    ///
    /// # Arguments
    /// * `user_id` - Unique identifier for the user/account
    /// * `display_name` - Optional human-readable name
    /// * `email` - Optional email address
    pub fn new(
        user_id: &str,
        display_name: Option<String>,
        email: Option<String>,
    ) -> Self {
        Self {
            user_id: user_id.to_string(),
            display_name,
            email,
        }
    }
}

/// Implement From for AuthIdentity conversion.
/// 
/// This allows converting from the auth system's identity type
/// to our workspace identity type.
impl From<crate::auth::AuthIdentity> for WorkspaceIdentity {
    fn from(auth: crate::auth::AuthIdentity) -> Self {
        Self {
            user_id: auth.user_id().unwrap_or_default().to_string(),
            display_name: auth.display_name().map(|s| s.to_string()),
            email: auth.email().map(|s| s.to_string()),
        }
    }
}

/// Captured workspace environment state.
///
/// This struct holds a snapshot of the workspace environment at a point in time,
/// including working directory, environment variables, git state, and other
/// context needed for debugging and reproduction.
///
/// # Serialization
///
/// The environment is serialized to JSON for upload to artifact storage.
/// Use [`WorkspaceEnvironment::to_json_bytes`] to get the serialized form.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkspaceEnvironment {
    /// When this capture was taken
    #[serde(default)]
    pub captured_at: String,
    
    /// Session ID that owns this environment
    #[serde(default)]
    pub session_id: String,
    
    /// Current working directory at time of capture
    #[serde(default)]
    pub cwd: PathBuf,
    
    /// Workspace identity information
    #[serde(default)]
    pub identity: WorkspaceIdentity,
    
    /// Server ID that processed this capture
    #[serde(default)]
    pub server_id: Option<String>,
    
    /// Sandbox ID if running in sandboxed mode
    #[serde(default)]
    pub sandbox_id: Option<String>,
    
    /// Operating system information
    #[serde(default)]
    pub os_info: OsInfo,
    
    /// Environment variable names (values are redacted for security)
    #[serde(default)]
    pub env_vars: Vec<String>,
    
    /// Git repository information (if in a git repo)
    #[serde(default)]
    pub git_info: Option<GitInfo>,
    
    /// Shell information
    #[serde(default)]
    pub shell_info: ShellInfo,
}

/// Operating system information snapshot.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct OsInfo {
    /// OS family (linux, macos, windows)
    #[serde(default)]
    pub family: String,
    
    /// OS version string
    #[serde(default)]
    pub version: String,
    
    /// Architecture (x86_64, aarch64, etc.)
    #[serde(default)]
    pub arch: String,
    
    /// Kernel version (Linux only)
    #[serde(default)]
    pub kernel_version: Option<String>,
}

/// Git repository information (if available).
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct GitInfo {
    /// Current branch name
    #[serde(default)]
    pub branch: Option<String>,
    
    /// Current commit hash (abbreviated)
    #[serde(default)]
    pub commit: Option<String>,
    
    /// Whether the working tree has uncommitted changes
    #[serde(default)]
    pub dirty: bool,
    
    /// Remote origin URL (redacted for security)
    #[serde(default)]
    pub remote_url: Option<String>,
    
    /// Root path of the git repository
    #[serde(default)]
    pub root_path: Option<PathBuf>,
}

/// Shell environment information.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ShellInfo {
    /// Shell program name (bash, zsh, fish, etc.)
    #[serde(default)]
    pub shell: String,
    
    /// Shell version string
    #[serde(default)]
    pub version: Option<String>,
    
    /// Current $SHELL environment variable value
    #[serde(default)]
    pub shell_path: Option<String>,
    
    /// Terminal type ($TERM)
    #[serde(default)]
    pub term: Option<String>,
}

impl WorkspaceEnvironment {
    /// Capture the current workspace environment.
    ///
    /// This function snapshots the workspace state including:
    /// - Working directory
    /// - OS information
    /// - Git repository state (if applicable)
    /// - Shell information
    /// - Environment variable names (not values, for security)
    ///
    /// # Arguments
    /// * `session_id` - The session identifier
    /// * `cwd` - The current working directory
    /// * `identity` - The workspace owner's identity
    /// * `server_id` - Optional server processing this request
    /// * `sandbox_id` - Optional sandbox identifier
    ///
    /// # Returns
    /// A populated `WorkspaceEnvironment` ready for serialization.
    ///
    /// # Panics
    /// This function should not panic under normal operation.
    /// Any errors during capture are logged but don't prevent
    /// the environment from being created with partial data.
    pub fn capture(
        session_id: &str,
        cwd: &Path,
        identity: &WorkspaceIdentity,
        server_id: Option<String>,
        sandbox_id: Option<String>,
    ) -> Self {
        let now = chrono::Utc::now().to_rfc3339();
        
        // Capture OS info
        let os_info = Self::capture_os_info();
        
        // Capture env var names (NOT values - security)
        let env_vars = Self::capture_env_var_names();
        
        // Capture git info (if in a git repo)
        let git_info = Self::capture_git_info(cwd);
        
        // Capture shell info
        let shell_info = Self::capture_shell_info();
        
        Self {
            captured_at: now,
            session_id: session_id.to_string(),
            cwd: cwd.to_path_buf(),
            identity: identity.clone(),
            server_id,
            sandbox_id,
            os_info,
            env_vars,
            git_info,
            shell_info,
        }
    }
    
    /// Serialize the environment to JSON bytes.
    ///
    /// # Returns
    /// `Ok(Vec<u8>)` containing the JSON representation on success.
    /// `Err(_)` if serialization fails (e.g., contains non-serializable data).
    pub fn to_json_bytes(&self) -> Result<Vec<u8>, serde_json::Error> {
        serde_json::to_vec_pretty(self)
    }
    
    /// Capture operating system information.
    fn capture_os_info() -> OsInfo {
        OsInfo {
            family: std::env::consts::FAMILY.to_string(),
            version: std::env::consts::OS.to_string(),
            arch: std::env::consts::ARCH.to_string(),
            kernel_version: Self::get_kernel_version(),
        }
    }
    
    /// Get kernel version on Linux systems.
    fn get_kernel_version() -> Option<String> {
        if cfg!(target_os = "linux") {
            std::fs::read_to_string("/proc/version")
                .ok()
                .map(|s| s.trim().to_string())
        } else {
            None
        }
    }
    
    /// Capture environment variable names (not values).
    ///
    /// We intentionally don't capture values to avoid leaking secrets
    /// like API keys, tokens, or passwords into artifact storage.
    fn capture_env_var_names() -> Vec<String> {
        std::env::vars()
            .map(|(key, _)| key)
            .collect()
    }
    
    /// Capture git repository information if cwd is in a git repo.
    fn capture_git_info(cwd: &Path) -> Option<GitInfo> {
        // Try to get git info using git2 library if available
        // Fall back to basic detection if not
        Self::try_git2_info(cwd).or_else(|| Self::basic_git_info(cwd))
    }
    
    /// Attempt to get detailed git info using git2.
    fn try_git2_info(cwd: &Path) -> Option<GitInfo> {
        let repo = git2::Repository::discover(cwd).ok()?;
        
        let head = repo.head().ok()?;
        let commit_hash = head.target()?.to_string();
        let branch = head.shorthand().map(|s| s.to_string());
        
        // Check if working tree is dirty
        let dirty = repo.statuses(None).map_or(false, |s| {
            s.iter().any(|e| {
                let status = e.status();
                status != git2::Status::CURRENT
            })
        });
        
        // Get remote URL (redact any auth tokens)
        let remote_url = repo
            .find_remote("origin")
            .ok()
            .and_then(|r| r.url().map(|s| s.to_string()))
            .map(Self::redact_git_url);
        
        // Get workdir path
        let root_path = repo.workdir().map(|p| p.to_path_buf());
        
        Some(GitInfo {
            branch,
            commit: Some(format!("{:.8}", commit_hash)),  // Abbreviate to 8 chars, wrapped in Some
            dirty,
            remote_url,
            root_path,
        })
    }
    
    /// Basic git info fallback when git2 is unavailable.
    fn basic_git_info(_cwd: &Path) -> Option<GitInfo> {
        // Return None - we need git2 for proper git info
        None
    }
    
    /// Redact sensitive information from git URLs.
    ///
    /// Removes embedded credentials from HTTPS URLs while preserving
    /// the host and path information.
    fn redact_git_url(url: String) -> String {
        // Remove user:password@ from https:// URLs
        if url.contains('@') && url.starts_with("https://") {
            if let Some(at_pos) = url.find('@') {
                if let Some(scheme_end) = url.find("://") {
                    let prefix = &url[..scheme_end + 3];
                    let suffix = &url[at_pos + 1..];
                    return format!("{}{}", prefix, suffix);
                }
            }
        }
        url
    }
    
    /// Capture shell/terminal information.
    fn capture_shell_info() -> ShellInfo {
        ShellInfo {
            shell: std::env::var("SHELL")
                .unwrap_or_else(|_| "unknown".to_string()),
            version: None,  // Would need to run shell --version
            shell_path: std::env::var("SHELL").ok(),
            term: std::env::var("TERM").ok(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_workspace_identity_default() {
        let identity = WorkspaceIdentity::default();
        assert!(identity.user_id.is_empty());
        assert!(identity.display_name.is_none());
        assert!(identity.email.is_none());
    }
    
    #[test]
    fn test_workspace_identity_new() {
        let identity = WorkspaceIdentity::new(
            "user-123",
            Some("Test User".to_string()),
            Some("test@example.com".to_string()),
        );
        assert_eq!(identity.user_id, "user-123");
        assert_eq!(identity.display_name.as_deref(), Some("Test User"));
        assert_eq!(identity.email.as_deref(), Some("test@example.com"));
    }
    
    #[test]
    fn test_environment_capture() {
        let identity = WorkspaceIdentity::new("test-user", None, None);
        let env = WorkspaceEnvironment::capture(
            "session-test",
            Path::new("/tmp"),
            &identity,
            Some("server-1".to_string()),
            None,
        );
        
        assert_eq!(env.session_id, "session-test");
        assert_eq!(env.cwd, Path::new("/tmp"));
        assert!(!env.captured_at.is_empty());
        assert_eq!(env.server_id.as_deref(), Some("server-1"));
    }
    
    #[test]
    fn test_environment_serialization() {
        let identity = WorkspaceIdentity::default();
        let env = WorkspaceEnvironment::capture(
            "test-session",
            Path::new("/home/test"),
            &identity,
            None,
            None,
        );
        
        let bytes = env.to_json_bytes().expect("Serialization should succeed");
        assert!(!bytes.is_empty());
        
        // Verify it's valid JSON
        let parsed: serde_json::Value =
            serde_json::from_slice(&bytes).expect("Should be valid JSON");
        assert!(parsed.get("session_id").is_some());
        assert!(parsed.get("captured_at").is_some());
    }
    
    #[test]
    fn test_redact_git_url_with_credentials() {
        let url = "https://oauth2:token123@github.com/org/repo.git".to_string();
        let redacted = WorkspaceEnvironment::redact_git_url(url);
        assert!(!redacted.contains("token123"));
        assert!(redacted.contains("github.com"));
    }
    
    #[test]
    fn test_redact_git_url_without_credentials() {
        let url = "https://github.com/org/repo.git".to_string();
        let redacted = WorkspaceEnvironment::redact_git_url(url);
        assert_eq!(redacted, url);
    }
}
