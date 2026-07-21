//! Authentication identity and provider types for workspace operations.
//!
//! This module defines the authentication abstractions used by the
//! workspace upload system to identify users and manage credentials.

/// Represents an authenticated user's identity.
///
/// This struct holds identifying information about the current user,
/// extracted from authentication tokens or OAuth flows.
#[derive(Debug, Clone, Default)]
pub struct AuthIdentity {
    /// Unique user identifier (e.g., "user-123" or "account:xxx")
    user_id: Option<String>,
    
    /// Human-readable display name
    display_name: Option<String>,
    
    /// Email address
    email: Option<String>,
    
    /// Whether this is a team/organization account
    is_team: bool,
    
    /// Team ID (if team account)
    team_id: Option<String>,
}

impl AuthIdentity {
    /// Create a new auth identity.
    pub fn new(
        user_id: Option<String>,
        display_name: Option<String>,
        email: Option<String>,
    ) -> Self {
        Self {
            user_id,
            display_name,
            email,
            is_team: false,
            team_id: None,
        }
    }
    
    /// Create a team identity.
    pub fn new_team(
        team_id: String,
        display_name: Option<String>,
    ) -> Self {
        Self {
            user_id: None,
            display_name,
            email: None,
            is_team: true,
            team_id: Some(team_id),
        }
    }
    
    /// Get the user ID.
    pub fn user_id(&self) -> Option<&str> {
        self.user_id.as_deref()
    }
    
    /// Get the display name.
    pub fn display_name(&self) -> Option<&str> {
        self.display_name.as_deref()
    }
    
    /// Get the email address.
    pub fn email(&self) -> Option<&str> {
        self.email.as_deref()
    }
    
    /// Check if this is a team account.
    pub fn is_team(&self) -> bool {
        self.is_team
    }
    
    /// Get the team ID (if team account).
    pub fn team_id(&self) -> Option<&str> {
        self.team_id.as_deref()
    }
}

/// Trait for authentication providers.
///
/// Implementors can provide identity information and manage
/// authentication credentials for API calls.
pub trait AuthProvider: Send + Sync {
    /// Get the current authenticated identity.
    fn identity(&self) -> &AuthIdentity;
    
    /// Get the user ID (convenience method).
    fn user_id(&self) -> Option<&str> {
        self.identity().user_id()
    }
    
    /// Get the display name (convenience method).
    fn display_name(&self) -> Option<&str> {
        self.identity().display_name()
    }
    
    /// Get the email address (convenience method).
    fn email(&self) -> Option<&str> {
        self.identity().email()
    }
}

/// A shared, thread-safe reference to an auth provider.
pub type SharedAuthProvider = std::sync::Arc<dyn AuthProvider>;

#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_auth_identity_default() {
        let identity = AuthIdentity::default();
        assert!(identity.user_id().is_none());
        assert!(identity.display_name().is_none());
        assert!(!identity.is_team());
    }
    
    #[test]
    fn test_auth_identity_new() {
        let identity = AuthIdentity::new(
            Some("user-123".to_string()),
            Some("Test User".to_string()),
            Some("test@example.com".to_string()),
        );
        
        assert_eq!(identity.user_id(), Some("user-123"));
        assert_eq!(identity.display_name(), Some("Test User"));
        assert_eq!(identity.email(), Some("test@example.com"));
        assert!(!identity.is_team());
    }
    
    #[test]
    fn test_auth_identity_team() {
        let identity = AuthIdentity::new_team(
            "team-456".to_string(),
            Some("Team Name".to_string()),
        );
        
        assert!(identity.is_team());
        assert_eq!(identity.team_id(), Some("team-456"));
        assert!(identity.user_id().is_none());
    }
    
    #[test]
    fn test_auth_provider_trait() {
        struct TestProvider {
            identity: AuthIdentity,
        }
        
        impl AuthProvider for TestProvider {
            fn identity(&self) -> &AuthIdentity {
                &self.identity
            }
        }
        
        let provider = TestProvider {
            identity: AuthIdentity::new(
                Some("test-user".to_string()),
                None,
                None,
            ),
        };
        
        assert_eq!(provider.user_id(), Some("test-user"));
    }
}
