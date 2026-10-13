# Security policy

pve-manager holds credentials for your Proxmox VE hosts, so please report vulnerabilities privately.

- Use GitHub's **private vulnerability reporting** for this repository ("Security" tab → "Report a vulnerability").
- Do not open a public issue for a security problem.
- Include the version (`pve-manager version`), what you did, and what you expected.

Only the latest release receives security fixes. The security model, including what the service token and the SSH
account can do on a host, is described in [docs/security.md](docs/security.md).
