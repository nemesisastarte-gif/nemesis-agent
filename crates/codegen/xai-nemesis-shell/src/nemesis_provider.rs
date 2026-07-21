//! NEMESIS Provider Configuration Module
//!
//! Reads ~/.nemesis/auth_config.json and applies the provider settings
//! to the application's environment and configuration.
//!
//! This allows users to configure custom AI providers (NVIDIA NIM, Groq,
//! Fireworks, or any OpenAI-compatible API) via the /auth command.

use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use tracing::info;

/// Provider configuration saved by /auth command
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NemesisProviderConfig {
    /// Provider type: "nvidia-nim", "groq", "fireworks", "custom", "grok"
    #[serde(default)]
    pub provider: String,
    
    /// API key for authentication
    #[serde(default)]
    pub api_key: String,
    
    /// Base URL for the API
    #[serde(default)]
    pub base_url: String,
    
    /// When this configuration was created/updated
    #[serde(default)]
    pub configured_at: Option<String>,
}

impl Default for NemesisProviderConfig {
    fn default() -> Self {
        Self {
            provider: String::new(),
            api_key: String::new(),
            base_url: String::new(),
            configured_at: None,
        }
    }
}

impl NemesisProviderConfig {
    /// Get the path to the auth configuration file
    pub fn config_path() -> PathBuf {
        let home = dirs::home_dir().unwrap_or_else(|| PathBuf::from("."));
        home.join(".nemesis").join("auth_config.json")
    }
    
    /// Load provider configuration from disk
    pub fn load() -> Option<Self> {
        let path = Self::config_path();
        if !path.exists() {
            return None;
        }
        
        std::fs::read_to_string(&path)
            .ok()
            .and_then(|content| serde_json::from_str(&content).ok())
    }
    
    /// Save provider configuration to disk
    pub fn save(&self) -> anyhow::Result<()> {
        let path = Self::config_path();
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        let content = serde_json::to_string_pretty(self)?;
        std::fs::write(&path, content)?;
        Ok(())
    }
    
    /// Check if a valid provider is configured
    pub fn is_configured(&self) -> bool {
        !self.provider.is_empty() && !self.api_key.is_empty()
    }
    
    /// Apply this provider configuration to environment variables
    ///
    /// This sets the environment variables that Grok/NEMESIS reads to configure
    /// its API endpoint and authentication:
    ///
    /// - `XAI_API_KEY` → Used for Bearer auth on API requests
    /// - `GROK_XAI_API_BASE_URL` → Overrides the default xAI API URL
    /// - `GROK_MODELS_BASE_URL` → Sets custom models endpoint
    /// - `GROK_CLI_CHAT_PROXY_BASE_URL` → Sets the chat proxy URL
    pub fn apply_to_environment(&self) -> bool {
        if !self.is_configured() {
            return false;
        }
        
        info!(provider = %self.provider, base_url = %self.base_url, 
             "Applying NEMESIS provider configuration");
        
        // Set API key as XAI_API_KEY (used by the sampler client)
        std::env::set_var("XAI_API_KEY", &self.api_key);
        
        // Also set as NEMESIS_API_KEY for reference
        std::env::set_var("NEMESIS_API_KEY", &self.api_key);
        
        // Override the API base URL based on provider
        match self.provider.as_str() {
            "nvidia-nim" => {
                let url = if self.base_url.is_empty() || self.base_url == "https://integrate.api.nvidia.com/v1" {
                    "https://integrate.api.nvidia.com/v1".to_string()
                } else {
                    self.base_url.clone()
                };
                
                // NVIDIA NIM uses OpenAI-compatible format but with different URL structure
                std::env::set_var("GROK_XAI_API_BASE_URL", &url);
                std::env::set_var("GROK_MODELS_BASE_URL", &url);
                std::env::set_var("GROK_CLI_CHAT_PROXY_BASE_URL", &url);
            }
            
            "groq" => {
                let url = if self.base_url.is_empty() || self.base_url == "https://api.groq.com/openai/v1" {
                    "https://api.groq.com/openai/v1".to_string()
                } else {
                    self.base_url.clone()
                };
                
                std::env::set_var("GROK_XAI_API_BASE_URL", &url);
                std::env::set_var("GROK_MODELS_BASE_URL", &url);
                std::env::set_var("GROK_CLI_CHAT_PROXY_BASE_URL", &url);
            }
            
            "fireworks" => {
                let url = if self.base_url.is_empty() || self.base_url == "https://api.fireworks.ai/inference/v1" {
                    "https://api.fireworks.ai/inference/v1".to_string()
                } else {
                    self.base_url.clone()
                };
                
                std::env::set_var("GROK_XAI_API_BASE_URL", &url);
                std::env::set_var("GROK_MODELS_BASE_URL", &url);
                std::env::set_var("GROK_CLI_CHAT_PROXY_BASE_URL", &url);
            }
            
            "custom" => {
                if !self.base_url.is_empty() {
                    std::env::set_var("GROK_XAI_API_BASE_URL", &self.base_url);
                    std::env::set_var("GROK_MODELS_BASE_URL", &self.base_url);
                    std::env::set_var("GROK_CLI_CHAT_PROXY_BASE_URL", &self.base_url);
                }
            }
            
            "grok" | _ => {
                // For Grok, use defaults (don't override)
                // The user will authenticate via OAuth flow
                std::env::remove_var("GROK_XAI_API_BASE_URL");
                std::env::remove_var("GROK_MODELS_BASE_URL");
                std::env::remove_var("GROK_CLI_CHAT_PROXY_BASE_URL");
            }
        }
        
        true
    }
    
    /// Get the list of available models for this provider
    pub fn get_default_models(&self) -> Vec<String> {
        match self.provider.as_str() {
            "nvidia-nim" => vec![
                "meta/llama-3.1-8b-instruct".to_string(),
                "meta/llama-3.1-70b-instruct".to_string(),
                "meta/llama-3.3-70b-instruct".to_string(),
                "mistralai/mixtral-8x22b-instruct-v0.1".to_string(),
                "databricks/dbrx-instruct".to_string(),
            ],
            "groq" => vec![
                "llama-3.1-8b-instant".to_string(),
                "llama-3.1-70b-versatile".to_string(),
                "mixtral-8x7b-32768".to_string(),
                "gemma2-9b-it".to_string(),
            ],
            "fireworks" => vec![
                "accounts/fireworks/models/llama-v3p1-70b-instruct".to_string(),
                "accounts/fireworks/models/llama-v3p1-8b-instruct".to_string(),
                "accounts/fireworks/models/mixtral-8x7b-instruct".to_string(),
            ],
            "custom" => vec![
                // For custom providers, we don't know the models - user must specify
            ],
            "grok" | _ => vec![
                "grok-4".to_string(),
                "grok-4.5".to_string(),
                "grok-4.5-low".to_string(),
            ],
        }
    }
    
    /// Get display name for the provider
    pub fn display_name(&self) -> &'static str {
        match self.provider.as_str() {
            "nvidia-nim" => "NVIDIA NIM",
            "groq" => "Groq",
            "fireworks" => "Fireworks AI",
            "custom" => "Custom API",
            "grok" | _ => "Grok (xAI)",
        }
    }
}

/// Initialize NEMESIS provider from saved configuration
///
/// Call this early in startup (before loading config) to apply any
/// saved provider settings to the environment.
pub fn initialize_nemesis_provider() -> Option<NemesisProviderConfig> {
    let config = NemesisProviderConfig::load()?;
    
    if config.is_configured() {
        config.apply_to_environment();
        info!(
            provider = %config.provider,
            display_name = %config.display_name(),
            "NEMESIS provider initialized"
        );
        Some(config)
    } else {
        None
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    
    #[test]
    fn test_default_config_is_not_configured() {
        let cfg = NemesisProviderConfig::default();
        assert!(!cfg.is_configured());
    }
    
    #[test]
    fn test_valid_config_is_configured() {
        let cfg = NemesisProviderConfig {
            provider: "nvidia-nim".to_string(),
            api_key: "test-key-12345".to_string(),
            base_url: "https://integrate.api.nvidia.com/v1".to_string(),
            configured_at: Some("2024-01-01T00:00:00Z".to_string()),
        };
        assert!(cfg.is_configured());
    }
    
    #[test]
    fn test_nvidia_nim_models() {
        let cfg = NemesisProviderConfig {
            provider: "nvidia-nim".to_string(),
            ..Default::default()
        };
        let models = cfg.get_default_models();
        assert!(!models.is_empty());
        assert!(models.iter().any(|m| m.contains("llama")));
    }
    
    #[test]
    fn test_display_names() {
        assert_eq!(
            NemesisProviderConfig { provider: "nvidia-nim".to_string(), ..Default::default() }.display_name(),
            "NVIDIA NIM"
        );
        assert_eq!(
            NemesisProviderConfig { provider: "groq".to_string(), ..Default::default() }.display_name(),
            "Groq"
        );
    }
}
