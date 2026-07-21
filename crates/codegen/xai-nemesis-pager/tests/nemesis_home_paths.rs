//! `NEMESIS_HOME` override tests in an isolated binary so `nemesis_home()`'s
//! process-wide `OnceLock` initializes from the overridden env var.

use std::path::PathBuf;

#[test]
fn nemesis_home_override_path_helpers() {
    let tmp = tempfile::tempdir().expect("tempdir");
    let nemesis_home = tmp.path().to_path_buf();
    unsafe {
        std::env::set_var("NEMESIS_HOME", &nemesis_home);
    }

    assert_eq!(
        xai_nemesis_pager::util::pager_toml_path(),
        nemesis_home.join("pager.toml")
    );
    assert_eq!(
        xai_nemesis_pager::util::display_nemesis_home_prefix(),
        "$NEMESIS_HOME"
    );
    assert_eq!(
        xai_nemesis_pager::util::display_user_grok_path("config.toml"),
        "$NEMESIS_HOME/config.toml"
    );

    let memory_path = nemesis_home.join("memory/MEMORY.md");
    assert_eq!(
        xai_nemesis_pager::util::abbreviate_path(&memory_path.display().to_string()),
        "$NEMESIS_HOME/memory/MEMORY.md"
    );

    // Copy-toast paths follow the same abbreviation convention, so a custom
    // $NEMESIS_HOME outside $HOME still displays short.
    assert_eq!(
        xai_nemesis_pager::clipboard::display_copy_path(&nemesis_home.join("last-copy.txt")),
        "$NEMESIS_HOME/last-copy.txt"
    );

    assert!(xai_nemesis_pager::util::is_under_user_nemesis_home(&memory_path));
    assert!(!xai_nemesis_pager::util::is_under_user_nemesis_home(
        PathBuf::from("/tmp/other").as_path()
    ));
}
