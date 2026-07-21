//! Shared utilities used by both `xai-nemesis-shell` and its downstream clients
//! (e.g. `xai-nemesis-pager-render`). This crate sits upstream of `xai-nemesis-shell`
//! so it must never depend on it.

pub mod clipboard;
pub mod placeholder_images;
pub mod session;
pub mod stderr;
pub mod ui_config;
