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
    
    /// Create a WorkspaceIdentity from any type that provides identity methods.
    /// 
    /// This is a generic constructor that works with various auth identity types
    /// including xai_computer_hub_sdk::AuthIdentity and our local AuthIdentity.
    pub fn from_identity<T>(identity: &T) -> Self 
    where
        T: IdentityProvider,
    {
        Self {
            user_id: identity.get_user_id().unwrap_or_default().to_string(),
            display_name: identity.get_display_name().map(|s| s.to_string()),
            email: identity.get_email().map(|s| s.to_string()),
        }
    }
}

/// Trait for types that can provide identity information.
///
/// Implemented for both local AuthIdentity and external auth types.
pub trait IdentityProvider {
    /// Get the user ID
    fn get_user_id(&self) -> Option<&str>;
    
    /// Get the display name
    fn get_display_name(&self) -> Option<&str>;
    
    /// Get the email address
    fn get_email(&self) -> Option<&str>;
}

// Note: IdentityProvider is implemented for crate::auth::AuthIdentity in the auth module itself
// to avoid circular dependency issues with super:: references

/// Captured workspace environment state.
///
/// This struct holds a snapshot of the workspace environment at a point in time,
/// including working directory, environment variables, git state, and other
/// context needed for debugging and reproduction.
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
    /// # Arguments
    /// * `session_id` - The session identifier
    /// * `cwd` - The current working directory
    /// * `identity` - The workspace owner's identity
    /// * `server_id` - Optional server processing this request
    /// * `sandbox_id` - Optional sandbox identifier
    pub fn capture(
        session_id: &str,
        cwd: &Path,
        identity: &WorkspaceIdentity,
        server_id: Option<String>,
        sandbox_id: Option<String>,
    ) -> Self {
        let now = chrono::Utc::now().to_rfc3339();
        
        Self {
            captured_at: now,
            session_id: session_id.to_string(),
            cwd: cwd.to_path_buf(),
            identity: identity.clone(),
            server_id,
            sandbox_id,
            os_info: Self::capture_os_info(),
            env_vars: Self::capture_env_var_names(),
            git_info: Self::capture_git_info(cwd),
            shell_info: Self::capture_shell_info(),
        }
    }
    
    /// Serialize the environment to JSON bytes.
    pub fn to_json_bytes(&self) -> Result<Vec<u8>, serde_json::Error> {
        serde_json::to_vec_pretty(self)
    }
    
    fn capture_os_info() -> OsInfo {
        OsInfo {
            family: std::env::consts::FAMILY.to_string(),
            version: std::env::consts::OS.to_string(),
            arch: std::env::consts::ARCH.to_string(),
            kernel_version: Self::get_kernel_version(),
        }
    }
    
    fn get_kernel_version() -> Option<String> {
        if cfg!(target_os = "linux") {
            std::fs::read_to_string("/proc/version")
                .ok()
                .map(|s| s.trim().to_string())
        } else {
            None
        }
    }
    
    fn capture_env_var_names() -> Vec<String> {
        std::env::vars()
            .map(|(key, _)| key)
            .collect()
    }
    
    fn capture_git_info(cwd: &Path) -> Option<GitInfo> {
        Self::try_git2_info(cwd)
    }
    
    fn try_git2_info(cwd: &Path) -> Option<GitInfo> {
        let repo = git2::Repository::discover(cwd).ok()?;
        
        let head = repo.head().ok()?;
        let commit_hash = head.target()?.to_string();
        let branch = head.shorthand().map(|s| s.to_string());
        
        let dirty = repo.statuses(None).map_or(false, |s| {
            s.iter().any(|e| e.status() != git2::Status::CURRENT)
        });
        
        let remote_url = repo
            .find_remote("origin")
            .ok()
            .and_then(|r| r.url().map(|s| s.to_string()))
            .map(Self::redact_git_url);
        
        let root_path = repo.workdir().map(|p| p.to_path_buf());
        
        Some(GitInfo {
            branch,
            commit: Some(format!("{:.8}", commit_hash)),
            dirty,
            remote_url,
            root_path,
        })
    }
    
    fn redact_git_url(url: String) -> String {
        if url.contains('@') && url.starts_with("https://") {
            if let Some(at_pos) = url.find('@') {
                if let Some(scheme_end) = url.find("://") {
                    return format!("{}{}", &url[..scheme_end + 3], &url[at_pos + 1..]);
                }
            }
        }
        url
    }
    
    fn capture_shell_info() -> ShellInfo {
        ShellInfo {
            shell: std::env::var("SHELL").unwrap_or_else(|_| "unknown".to_string()),
            version: None,
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
    }
    
    #[test]
    fn test_workspace_identity_new() {
        let identity = WorkspaceIdentity::new("user-123", Some("Test".to_string()), Some("test@ex.com".to_string()));
        assert_eq!(identity.user_id, "user-123");
    }
    
    #[test]
    fn test_environment_capture() {
        let env = WorkspaceEnvironment::capture(
            "session-test",
            Path::new("/tmp"),
            &WorkspaceIdentity::default(),
            None,
            None,
        );
        assert_eq!(env.session_id, "session-test");
    }
    
    #[test]
    fn test_environment_serialization() {
        let env = WorkspaceEnvironment::capture("test", Path::new("/home"), &WorkspaceIdentity::default(), None, None);
        let bytes = env.to_json_bytes().expect("Serialization should succeed");
        assert!(!bytes.is_empty());
    }
}
