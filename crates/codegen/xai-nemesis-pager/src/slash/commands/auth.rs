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
use xai_nemesis_shell::nemesis_provider::{NemesisProviderConfig, initialize_nemesis_provider};

/// Available AI providers for NEMESIS Agent
#[derive(Debug, Clone)]
pub enum AuthProvider {
    /// xAI Grok (OAuth browser flow)
    Grok,
    /// NVIDIA NIM (API key)
    NvidiaNim,
    /// Groq (API key)
    Groq,
    /// Fireworks (API key)
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

    /// Provider ID string for storage
    pub fn id(&self) -> &'static str {
        match self {
            Self::Grok => "grok",
            Self::NvidiaNim => "nvidia-nim",
            Self::Groq => "groq",
            Self::Fireworks => "fireworks",
            Self::Custom => "custom",
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
        // Load current config using the shared module
        let config = NemesisProviderConfig::load();
        
        let status = match config {
            Some(ref cfg) if cfg.is_configured() => {
                format!(
                    "Current Configuration:\n  Provider: {}\n  Base URL:  {}\n  API Key:   {}…{}\n",
                    cfg.display_name(),
                    if cfg.base_url.is_empty() { "(default)" } else { &cfg.base_url },
                    &cfg.api_key[..std::cmp::min(8, cfg.api_key.len())],
                    if cfg.configured_at.is_some() { 
                        format!("\n  Configured: {}", cfg.configured_at.as_deref().unwrap_or("unknown")) 
                    } else { 
                        String::new() 
                    }
                )
            }
            None => "No configuration found.\n".to_string(),
            _ => "Configuration exists but is incomplete.\n".to_string(),
        };

        CommandResult::Message(format!(
            "{status}\n\
            Usage:\n\
            /auth <provider> [api_key] [base_url]\n\
            \n\
            Providers:\n{}\n\
            \n\
            Examples:\n\
            /auth grok\n\
            /auth nvidia-nim nvapi-xxxxxxxxxxxx\n\
            /auth groq gsk_yyyyyyyyyyyyyyyy\n\
            /auth custom sk-xxx https://my-api.example.com/v1\n\
            \n\
            Note: After configuring a provider, restart NEMESIS or start\n\
            a new session for changes to take full effect.",
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

    /// Configure the selected provider and save to disk
    fn configure_provider(
        &self,
        provider: AuthProvider,
        api_key: Option<String>,
        base_url: Option<String>,
    ) -> CommandResult {
        match provider {
            AuthProvider::Grok => {
                // For Grok, trigger the OAuth login flow
                // Clear any custom provider config first
                let _ = std::fs::remove_file(NemesisProviderConfig::config_path());
                
                CommandResult::Action(Action::Login)
            }
            other_provider => {
                self.save_api_key_provider(other_provider, api_key, base_url)
            }
        }
    }

    /// Save API key-based provider configuration using the shared module
    fn save_api_key_provider(
        &self,
        provider: AuthProvider,
        api_key: Option<String>,
        base_url: Option<String>,
    ) -> CommandResult {
        // Validate API key is present
        let api_key = match api_key {
            Some(key) if !key.is_empty() => key,
            _ => {
                return CommandResult::Error(format!(
                    "{} requires an API key.\n\
                    Usage: /auth {} <api_key>\n\
                    \n\
                    Get your API key from: {}",
                    provider.display_name(),
                    provider.id(),
                    Self::get_provider_docs_url(provider.id())
                ));
            }
        };

        // Determine base URL with default fallback
        let resolved_base_url = base_url
            .filter(|u| !u.is_empty())
            .or_else(|| provider.default_base_url().map(|s| s.to_string()))
            .unwrap_or_default();

        // Create config using the shared module
        let config = NemesisProviderConfig {
            provider: provider.id().to_string(),
            api_key: api_key.clone(),
            base_url: resolved_base_url.clone(),
            configured_at: Some(chrono::Utc::now().to_rfc3339()),
        };

        // Save to disk
        match config.save() {
            Ok(()) => {
                // Apply to environment immediately
                config.apply_to_environment();
                
                // Get available models for this provider
                let models = config.get_default_models();
                let models_info = if models.is_empty() {
                    String::new()
                } else {
                    format!("\n\nAvailable models:\n{}", 
                        models.iter().map(|m| format!("  • {m}")).collect::<Vec<_>>().join("\n"))
                };
                
                CommandResult::Message(format!(
                    "✓ Provider configured successfully!\n\
                    \n\
                    Provider:  {}\n\
                    Base URL:  {}\n\
                    API Key:   {}…{}\n\
                    \n\
                    Configuration saved to ~/.nemesis/auth_config.json\n\
                    Environment variables updated.{}
                    \n\
                    💡 Start a new session or send a message to use this provider.",
                    provider.display_name(),
                    if resolved_base_url.is_empty() { "(default)" } else { &resolved_base_url },
                    &api_key[..std::cmp::min(8, api_key.len())],
                    models_info,
                    if !models.is_empty() { "\n\nUse /model <name> to select a model." } else { "" }
                ))
            }
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
    
    #[test]
    fn test_provider_ids() {
        assert_eq!(AuthProvider::Grok.id(), "grok");
        assert_eq!(AuthProvider::NvidiaNim.id(), "nvidia-nim");
        assert_eq!(AuthProvider::Groq.id(), "groq");
        assert_eq!(AuthProvider::Fireworks.id(), "fireworks");
        assert_eq!(AuthProvider::Custom.id(), "custom");
    }
}
