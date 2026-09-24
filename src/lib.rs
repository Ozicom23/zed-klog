use std::fs;

use zed_extension_api::{self as zed, settings::LspSettings, LanguageServerId, Result};

const SERVER_NAME: &str = "klog-ls";
const GITHUB_REPOSITORY: &str = "Ozicom23/zed-klog";

struct KlogExtension {
    cached_binary_path: Option<String>,
}

impl KlogExtension {
    /// Finds the language server, in this order: the path configured in the
    /// settings, `klog-ls` on the PATH, or the latest GitHub release.
    fn server_command(
        &mut self,
        language_server_id: &LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<zed::Command> {
        let binary = LspSettings::for_worktree(SERVER_NAME, worktree)
            .ok()
            .and_then(|settings| settings.binary);
        let args = binary
            .as_ref()
            .and_then(|binary| binary.arguments.clone())
            .unwrap_or_default();

        let command = match binary.and_then(|binary| binary.path) {
            Some(path) => path,
            None => match worktree.which(SERVER_NAME) {
                Some(path) => path,
                None => self.downloaded_binary(language_server_id)?,
            },
        };
        Ok(zed::Command {
            command,
            args,
            env: Default::default(),
        })
    }

    fn downloaded_binary(&mut self, language_server_id: &LanguageServerId) -> Result<String> {
        if let Some(path) = &self.cached_binary_path {
            if fs::metadata(path).is_ok_and(|metadata| metadata.is_file()) {
                return Ok(path.clone());
            }
        }

        zed::set_language_server_installation_status(
            language_server_id,
            &zed::LanguageServerInstallationStatus::CheckingForUpdate,
        );
        let release = zed::latest_github_release(
            GITHUB_REPOSITORY,
            zed::GithubReleaseOptions {
                require_assets: true,
                pre_release: false,
            },
        )?;

        let (platform, arch) = zed::current_platform();
        let os = match platform {
            zed::Os::Mac => "darwin",
            zed::Os::Linux => "linux",
            zed::Os::Windows => "windows",
        };
        let arch = match arch {
            zed::Architecture::Aarch64 => "arm64",
            zed::Architecture::X8664 => "amd64",
            zed::Architecture::X86 => return Err("klog-ls does not support 32-bit x86".into()),
        };
        let (extension, file_type, executable) = match platform {
            zed::Os::Windows => ("zip", zed::DownloadedFileType::Zip, "klog-ls.exe"),
            _ => ("tar.gz", zed::DownloadedFileType::GzipTar, "klog-ls"),
        };

        let asset_name = format!("klog-ls-{os}-{arch}.{extension}");
        let asset = release
            .assets
            .iter()
            .find(|asset| asset.name == asset_name)
            .ok_or_else(|| format!("no release asset named {asset_name}"))?;

        let version_dir = format!("klog-ls-{}", release.version);
        let binary_path = format!("{version_dir}/{executable}");
        if !fs::metadata(&binary_path).is_ok_and(|metadata| metadata.is_file()) {
            zed::set_language_server_installation_status(
                language_server_id,
                &zed::LanguageServerInstallationStatus::Downloading,
            );
            zed::download_file(&asset.download_url, &version_dir, file_type)
                .map_err(|error| format!("failed to download {asset_name}: {error}"))?;
            zed::make_file_executable(&binary_path)?;

            // Remove older versions.
            for entry in fs::read_dir(".").map_err(|error| error.to_string())?.flatten() {
                let name = entry.file_name();
                let name = name.to_string_lossy();
                if name.starts_with("klog-ls-") && name != version_dir {
                    fs::remove_dir_all(entry.path()).ok();
                }
            }
        }

        self.cached_binary_path = Some(binary_path.clone());
        Ok(binary_path)
    }
}

impl zed::Extension for KlogExtension {
    fn new() -> Self {
        Self {
            cached_binary_path: None,
        }
    }

    fn language_server_command(
        &mut self,
        language_server_id: &LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<zed::Command> {
        self.server_command(language_server_id, worktree)
    }
}

zed::register_extension!(KlogExtension);
