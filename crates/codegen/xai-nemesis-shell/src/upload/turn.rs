//! Turn-level trace collection and upload.

use serde::{Deserialize, Serialize};

/// Request to generate and upload a synthetic turn trace.
///
/// Synthetic traces are generated for turns that don't produce natural
/// traces (e.g., internal operations, system prompts) but still need
/// to be represented in the trace stream for completeness.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SyntheticTurnTraceRequest {
    /// Session this turn belongs to
    pub session_id: String,
    
    /// Turn number
    pub turn_number: u64,
    
    /// Type of synthetic trace
    pub trace_type: SyntheticTraceType,
    
    /// When the trace was generated
    pub timestamp: chrono::DateTime<chrono::Utc>,
    
    /// Additional metadata for the trace
    pub metadata: serde_json::Value,
}

/// Types of synthetic traces that can be generated.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum SyntheticTraceType {
    /// System prompt application
    SystemPrompt,
    
    /// Tool result processing
    ToolResult,
    
    /// Internal state update
    StateUpdate,
    
    /// Error/recovery event
    ErrorRecovery,
    
    /// Session lifecycle event (start, end, etc.)
    LifecycleEvent,
    
    /// Custom synthetic trace
    Custom,
}

impl Default for SyntheticTraceType {
    fn default() -> Self {
        Self::Custom
    }
}

impl std::fmt::Display for SyntheticTraceType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::SystemPrompt => write!(f, "system_prompt"),
            Self::ToolResult => write!(f, "tool_result"),
            Self::StateUpdate => write!(f, "state_update"),
            Self::ErrorRecovery => write!(f, "error_recovery"),
            Self::LifecycleEvent => write!(f, "lifecycle_event"),
            Self::Custom => write!(f, "custom"),
        }
    }
}

impl SyntheticTurnTraceRequest {
    /// Create a new synthetic turn trace request.
    pub fn new(
        session_id: String,
        turn_number: u64,
        trace_type: SyntheticTraceType,
    ) -> Self {
        Self {
            session_id,
            turn_number,
            trace_type,
            timestamp: chrono::Utc::now(),
            metadata: serde_json::Value::Object(serde_json::Map::new()),
        }
    }
    
    /// Add metadata to this request.
    pub fn with_metadata(mut self, key: &str, value: serde_json::Value) -> Self {
        self.metadata[key] = value;
        self
    }
}
