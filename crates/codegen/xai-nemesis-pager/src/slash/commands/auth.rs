//! `/auth` -- Configure authentication provider for NEMESIS Agent.
//!
//! Allows users to select and configure their AI provider:
//! - Grok (xAI OAuth)
//! - NVIDIA NIM (API key)
//! - Groq (API key)
//! - Fireworks (API key)
//! - Custom OpenAI-compatible (base URL + API key)

use crate::app::actions::Action;
use crate::slash::command::{CommandExecCtx, CommandResult, SlashCommand};

/// Available AI providers for NEMESIS Agent
#[derive(Debug, Clone)]
pub enum AuthProvider {
    /// xAI Grok (OAuth browser flow)
    Grok,
    /// NVIDIA NIM (API key)
    NvidiaNim,
    /// Groq (API key)
    Groq,
    /// Fireworks AI (API key)
    Fireworks,
    /// Custom OpenAI-compatible endpoint
    Custom,
}

impl AuthProvider {
    /// Display name for the provider
    pub fn display_name(&self) -> &'static str {
        match self {
            Self::Grok => "Grok (xAI)",
            Self::NvidiaNim => "NVIDIA NIM",
            Self::Groq => "Groq",
            Self::Fireworks => "Fireworks",
            Self::Custom => "Custom OpenAI",
        }
    }

    /// Description for the provider
    pub fn description(&self) -> &'static str {
        match self {
            Self::Grok => "Authenticate via xAI Grok OAuth (browser login)",
            Self::NvidiaNim => "Use NVIDIA NIM API with an API key",
            Self::Groq => "Use Groq API with an API key",
            Self::Fireworks => "Use Fireworks AI API with an API key",
            Self::Custom => "Use any OpenAI-compatible API (custom URL + key)",
        }
    }

    /// Default base URL for the provider
    pub fn default_base_url(&self) -> Option<&'static str> {
        match self {
            Self::Grok => Some("https://api.x.ai/v1"),
            Self::NvidiaNim => Some("https://integrate.api.nvidia.com/v1"),
            Self::Groq => Some("https://api.groq.com/openai/v1"),
            Self::Fireworks => Some("https://api.fireworks.ai/inference/v1"),
            Self::Custom => None,
        }
    }

    /// Parse provider from string (case-insensitive)
    pub fn from_str(s: &str) -> Option<Self> {
        match s.to_lowercase().as_str() {
            "grok" | "xai" | "x.ai" => Some(Self::Grok),
            "nvidia" | "nim" | "nvidia-nim" => Some(Self::NvidiaNim),
            "groq" => Some(Self::Groq),
            "fireworks" | "fireworks-ai" => Some(Self::Fireworks),
            "custom" | "openai" | "openai-compatible" => Some(Self::Custom),
            _ => None,
        }
    }

    /// List all available providers
    pub fn all() -> Vec<AuthProvider> {
        vec![
            Self::Grok,
            Self::NvidiaNim,
            Self::Groq,
            Self::Fireworks,
            Self::Custom,
        ]
    }
}

pub struct AuthCommand;

impl SlashCommand for AuthCommand {
    fn name(&self) -> &str {
        "auth"
    }

    fn aliases(&self) -> &[&str] {
        &["provider", "configure"]
    }

    fn description(&self) -> &str {
        "Configure authentication provider (Grok, NIM, Groq, Fireworks, Custom)"
    }

    fn usage(&self) -> &str {
        "/auth <provider> [api_key] [base_url]"
    }

    fn takes_args(&self) -> bool {
        true
    }

    fn args_required(&self) -> bool {
        false // Show help when no args
    }

    fn suggest_args(&self, _ctx: &crate::slash::command::AppCtx, _args_query: &str) -> Option<Vec<crate::slash::command::ArgItem>> {
        let providers = AuthProvider::all();
        let items = providers.into_iter().map(|p| {
            crate::slash::command::ArgItem {
                display: p.display_name().to_string(),
                match_text: p.display_name().to_lowercase(),
                insert_text: p.display_name().to_string(),
                description: p.description().to_string(),
            }
        }).collect();
        Some(items)
    }

    fn run(&self, _ctx: &mut CommandExecCtx, args: &str) -> CommandResult {
        let args = args.trim();

        // No arguments: show current status and available providers
        if args.is_empty() {
            return self.show_status_and_help();
        }

        // Parse provider from first argument
        let parts: Vec<&str> = args.split_whitespace().collect();
        let provider_name = parts[0];

        match AuthProvider::from_str(provider_name) {
            Some(provider) => {
                // Extract optional api_key and base_url from remaining args
                let api_key = parts.get(1).map(|s| s.to_string());
                let base_url = parts.get(2).map(|s| s.to_string());

                self.configure_provider(provider, api_key, base_url)
            }
            None => CommandResult::Error(format!(
                "Unknown provider '{provider_name}'.\n\nAvailable providers:\n{}",
                Self::format_provider_list()
            )),
        }
    }
}

impl AuthCommand {
    /// Show current auth configuration status and usage help
    fn show_status_and_help(&self) -> CommandResult {
        let config = Self::load_current_config();
        
        let status = match config.as_object() {
            Some(obj) => {
                let provider = obj.get("provider")
                    .and_then(|v| v.as_str())
                    .unwrap_or("None configured");
                
                let has_key = obj.get("api_key")
                    .map(|v| if v.as_str().map_or(false, |s| !s.is_empty()) { "Yes" } else { "No" })
                    .unwrap_or("No");
                
                format!(
                    "Current Configuration:\n  Provider: {provider}\n  API Key:   {has_key}\n"
                )
            }
            None => "No configuration found.\n".to_string(),
        };

        CommandResult::Message(format!(
            "{status}\nUsage:\n\
            /auth <provider> [api_key] [base_url]\n\
            \n\
            Providers:\n{}\n\
            \n\
            Examples:\n\
            /auth grok\n\
            /auth nvidia-nim nvi_xxxxxxxxxxxx\n\
            /auth custom sk-xxx https://my-api.example.com/v1\n\
            \n\
            Use Tab after '/auth ' to see available providers.",
            Self::format_provider_list()
        ))
    }

    /// Format the list of available providers
    fn format_provider_list() -> String {
        AuthProvider::all()
            .iter()
            .map(|p| format!("  • {} - {}", p.display_name(), p.description()))
            .collect::<Vec<_>>()
            .join("\n")
    }

    /// Configure the selected provider
    fn configure_provider(
        &self,
        provider: AuthProvider,
        api_key: Option<String>,
        base_url: Option<String>,
    ) -> CommandResult {
        match provider {
            AuthProvider::Grok => {
                // For Grok, trigger the OAuth login flow
                CommandResult::Action(Action::Login)
            }
            AuthProvider::NvidiaNim => {
                self.save_api_key_provider("nvidia-nim", api_key, base_url, "https://integrate.api.nvidia.com/v1")
            }
            AuthProvider::Groq => {
                self.save_api_key_provider("groq", api_key, base_url, "https://api.groq.com/openai/v1")
            }
            AuthProvider::Fireworks => {
                self.save_api_key_provider("fireworks", api_key, base_url, "https://api.fireworks.ai/inference/v1")
            }
            AuthProvider::Custom => {
                // Custom requires both API key and base URL
                match (&api_key, &base_url) {
                    (Some(key), Some(url)) => {
                        if url.is_empty() || key.is_empty() {
                            return CommandResult::Error(
                                "Custom provider requires both API key and base URL.\n\
                                Usage: /auth custom <api_key> <base_url>".to_string()
                            );
                        }
                        self.save_api_key_provider("custom", api_key, base_url, "")
                    }
                    (None, _) => {
                        CommandResult::Error(
                            "Custom provider requires API key and base URL.\n\
                            Usage: /auth custom <api_key> <base_url>\n\
                            Example: /auth custom sk-xxx https://api.example.com/v1".to_string()
                        )
                    }
                    (_, None) => {
                        CommandResult::Error(
                            "Custom provider requires a base URL.\n\
                            Usage: /auth custom <api_key> <base_url>\n\
                            Example: /auth custom sk-xxx https://api.example.com/v1".to_string()
                        )
                    }
                }
            }
        }
    }

    /// Save API key-based provider configuration
    fn save_api_key_provider(
        &self,
        provider_id: &str,
        api_key: Option<String>,
        base_url: Option<String>,
        default_url: &str,
    ) -> CommandResult {
        // Validate API key is present
        let api_key = match api_key {
            Some(key) if !key.is_empty() => key,
            _ => {
                return CommandResult::Error(format!(
                    "{provider_id} requires an API key.\n\
                    Usage: /auth {provider_id} <api_key>\n\
                    \n\
                    Get your API key from: {}",
                    Self::get_provider_docs_url(provider_id)
                ));
            }
        };

        // Determine base URL
        let base_url = base_url
            .filter(|u| !u.is_empty())
            .unwrap_or_else(|| default_url.to_string());

        // Save configuration
        match Self::save_config(provider_id, &api_key, &base_url) {
            Ok(()) => CommandResult::Message(format!(
                "✓ Provider configured successfully!\n\
                \n\
                Provider:  {}\n\
                Base URL:  {}\n\
                API Key:   {}…\n\
                \n\
                You can now start chatting. Use /model to select a model.",
                provider_id,
                base_url,
                &api_key[..std::cmp::min(8, api_key.len())]
            )),
            Err(e) => CommandResult::Error(format!("Failed to save configuration: {e}")),
        }
    }

    /// Get documentation URL for provider
    fn get_provider_docs_url(provider_id: &str) -> &'static str {
        match provider_id {
            "nvidia-nim" => "https://build.nvidia.com/nim",
            "groq" => "https://console.groq.com/keys",
            "fireworks" => "https://fireworks.ai/account/api-keys",
            "custom" => "your API provider's documentation",
            _ => "the provider's website",
        }
    }

    /// Load current configuration from disk
    fn load_current_config() -> serde_json::Value {
        let config_path = Self::config_path();
        
        if !config_path.exists() {
            return serde_json::json!({});
        }

        std::fs::read_to_string(&config_path)
            .ok()
            .and_then(|content| serde_json::from_str(&content).ok())
            .unwrap_or(serde_json::json!({}))
    }

    /// Save provider configuration to disk
    fn save_config(provider_id: &str, api_key: &str, base_url: &str) -> anyhow::Result<()> {
        let config = Self::load_current_config();
        
        let mut new_config = config.clone();
        if let Some(obj) = new_config.as_object_mut() {
            obj.insert("provider".into(), serde_json::json!(provider_id));
            obj.insert("api_key".into(), serde_json::json!(api_key));
            obj.insert("base_url".into(), serde_json::json!(base_url));
            obj.insert("configured_at".into(), serde_json::json!(
                chrono::Utc::now().to_rfc3339()
            ));
        }

        let config_path = Self::config_path();
        
        // Ensure parent directory exists
        if let Some(parent) = config_path.parent() {
            std::fs::create_dir_all(parent)?;
        }

        std::fs::write(&config_path, serde_json::to_string_pretty(&new_config)?)?;
        
        tracing::info!(provider = provider_id, "Auth configuration saved");
        
        Ok(())
    }

    /// Get path to the auth configuration file
    fn config_path() -> std::path::PathBuf {
        // Use ~/.nemesis/auth_config.json
        let nemesis_home = dirs::home_dir()
            .unwrap_or_else(|| std::path::PathBuf::from("."))
            .join(".nemesis");
        
        nemesis_home.join("auth_config.json")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_auth_command_metadata() {
        let cmd = AuthCommand;
        assert_eq!(cmd.name(), "auth");
        assert!(cmd.aliases().contains(&"provider"));
        assert!(cmd.takes_args());
        assert!(!cmd.args_required()); // Can show help with no args
    }

    #[test]
    fn test_provider_parsing() {
        assert!(AuthProvider::from_str("grok").is_some());
        assert!(AuthProvider::from_str("GROK").is_some());
        assert!(AuthProvider::from_str("nvidia").is_some());
        assert!(AuthProvider::from_str("nim").is_some());
        assert!(AuthProvider::from_str("groq").is_some());
        assert!(AuthProvider::from_str("fireworks").is_some());
        assert!(AuthProvider::from_str("custom").is_some());
        assert!(AuthProvider::from_str("unknown").is_none());
    }

    #[test]
    fn test_provider_display_names() {
        assert_eq!(AuthProvider::Grok.display_name(), "Grok (xAI)");
        assert_eq!(AuthProvider::NvidiaNim.display_name(), "NVIDIA NIM");
        assert_eq!(AuthProvider::Groq.display_name(), "Groq");
        assert_eq!(AuthProvider::Fireworks.display_name(), "Fireworks");
        assert_eq!(AuthProvider::Custom.display_name(), "Custom OpenAI");
    }

    #[test]
    fn test_default_base_urls() {
        assert_eq!(AuthProvider::Grok.default_base_url(), Some("https://api.x.ai/v1"));
        assert_eq!(AuthProvider::NvidiaNim.default_base_url(), Some("https://integrate.api.nvidia.com/v1"));
        assert_eq!(AuthProvider::Groq.default_base_url(), Some("https://api.groq.com/openai/v1"));
        assert_eq!(AuthProvider::Fireworks.default_base_url(), Some("https://api.fireworks.ai/inference/v1"));
        assert_eq!(AuthProvider::Custom.default_base_url(), None);
    }

    #[test]
    fn test_no_args_shows_help() {
        use crate::slash::command::{CommandExecCtx, ModelState};
        let models = ModelState::default();
        let mut ctx = CommandExecCtx {
            models: &models,
            session_id: None,
            bundle_state: &crate::app::bundle::BundleState::default(),
            screen_mode: crate::app::ScreenMode::Fullscreen,
            pager_state: crate::settings::PagerLocalSnapshot::default(),
        };
        
        let cmd = AuthCommand;
        let result = cmd.run(&mut ctx, "");
        
        match result {
            CommandResult::Message(msg) => {
                assert!(msg.contains("Current Configuration") || msg.contains("Usage"));
                assert!(msg.contains("/auth"));
            }
            other => panic!("Expected Message result, got: {:?}", other),
        }
    }

    #[test]
    fn test_grok_triggers_login_action() {
        use crate::slash::command::{CommandExecCtx, ModelState};
        let models = ModelState::default();
        let mut ctx = CommandExecCtx {
            models: &models,
            session_id: None,
            bundle_state: &crate::app::bundle::BundleState::default(),
            screen_mode: crate::app::ScreenMode::Fullscreen,
            pager_state: crate::settings::PagerLocalSnapshot::default(),
        };
        
        let cmd = AuthCommand;
        let result = cmd.run(&mut ctx, "grok");
        
        match result {
            CommandResult::Action(action) => {
                assert!(matches!(action, Action::Login), "Expected Login action for Grok");
            }
            other => panic!("Expected Action(Login), got: {:?}", other),
        }
    }

    #[test]
    fn test_unknown_provider_error() {
        use crate::slash::command::{CommandExecCtx, ModelState};
        let models = ModelState::default();
        let mut ctx = CommandExecCtx {
            models: &models,
            session_id: None,
            bundle_state: &crate::app::bundle::BundleState::default(),
            screen_mode: crate::app::ScreenMode::Fullscreen,
            pager_state: crate::settings::PagerLocalSnapshot::default(),
        };
        
        let cmd = AuthCommand;
        let result = cmd.run(&mut ctx, "unknown_provider");
        
        match result {
            CommandResult::Error(msg) => {
                assert!(msg.contains("Unknown provider"));
            }
            other => panic!("Expected Error, got: {:?}", other),
        }
    }
}
