//! Turn-level trace collection and upload.
//!
//! This module handles synthetic turn trace generation for events that don't
//! produce natural traces (subagent completions, bash task completions, etc.)

use tokio::sync::oneshot;

/// Request to generate and upload a synthetic turn trace.
///
/// Synthetic traces are generated for turns that don't produce natural
/// traces (e.g., subagent completions, internal operations) but still need
/// to be represented in the trace stream for completeness.
///
/// Sent by the notification bridge (for bash task completions) or the
/// subagent coordinator (for subagent completions) to the synthetic trace handler.
pub struct SyntheticTurnTraceRequest {
    /// Session this turn belongs to
    pub session_id: agent_client_protocol::SessionId,
    
    /// Unique identifier for the prompt that triggered this trace
    pub prompt_id: String,
    
    /// Completion channel receiver - fires when the prompt turn completes
    pub completion_rx: oneshot::Receiver<crate::session::commands::PromptTurnResult>,
    
    /// Before session copy channel receiver - fires when session state is copied
    pub before_session_copy_rx: oneshot::Receiver<anyhow::Result<crate::session::persistence::SessionStateCopy>>,
}
