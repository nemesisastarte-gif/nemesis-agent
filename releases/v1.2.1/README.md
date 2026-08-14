# NemesisCode 1.2.1

Correctif du moteur local : le backend détecte automatiquement opencode dans
`/usr/share/nemesiscode/opencode`, migre l'ancien défaut `ohmyagent` et garde
la tâche interactive après chaque fin de tour.

```bash
curl -fLO https://github.com/nemesisastarte-gif/nemesis-agent/raw/v1.2.1/releases/v1.2.1/nemesiscode_1.2.1_amd64.deb
curl -fLO https://github.com/nemesisastarte-gif/nemesis-agent/raw/v1.2.1/releases/v1.2.1/SHA256SUMS
sha256sum -c SHA256SUMS
sudo dpkg -i nemesiscode_1.2.1_amd64.deb
nemesiscode on
nemesiscode engine
```
