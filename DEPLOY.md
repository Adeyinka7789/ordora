# Ordora — Production Deploy (Truehost VPS)

One pipeline file, three stages that run in order (`.github/workflows/pipeline.yml`):

```
push to main → [test] gofmt, vet, build, migrate + tests on Postgres 16
             → [build] linux binaries + release tarball (needs test to pass)
             → [deploy] ship to VPS, migrate, flip release, restart, health-check
```

Pull requests run test → build only (deploy is main-branch only). You can also
run the whole pipeline manually from the Actions tab (Run workflow).

## Layout on the VPS

```
/opt/ordora/releases/<sha>/   one dir per deploy (binaries, migrations, templates, static)
 /opt/ordora/current -> releases/<sha>   (what systemd runs)
/opt/ordora/shared/uploads     persistent uploads (never touched by deploys)
/opt/ordora/shared/contact.json  editable contact details (never touched by deploys)
/etc/ordora/ordora.env         secrets + config (root-created, ordora-readable)
```

systemd: `ordora-server` + `ordora-worker`. Reverse proxy: Caddy (`https://DOMAIN`
→ `127.0.0.1:8080`, auto-TLS). Existing apps on the VPS are untouched: the script
only adds its own DB/roles, users, units and one Caddy site file.

## One-time setup

1. **DNS**: A record for your domain → VPS IP.
2. **Copy + run provision** (as root on the VPS):
   ```bash
   scp scripts/provision-vps.sh root@VPS:/tmp/
   ssh root@VPS
   DOMAIN=ordora.example.com APP_PORT=8080 bash /tmp/provision-vps.sh
   ```
   Optional overrides: `DB_NAME=ordora` (default). Re-running is safe.
3. **Fill in secrets on the VPS**:
   - `/etc/ordora/ordora.env` — SMTP settings, `ORDORA_ADMIN_PATH` (long random path).
   - `/opt/ordora/shared/contact.json` — real emails/phones/address/WhatsApp.
4. **Deploy user key**: generate a keypair locally (`ssh-keygen -t ed25519 -f ~/.ssh/ordora-deploy`),
   append the `.pub` to `/home/deploy/.ssh/authorized_keys` on the VPS.
5. **GitHub secrets** (repo Settings → Secrets → Actions): `VPS_HOST`, `VPS_USER=deploy`,
   `VPS_SSH_KEY` (private key), plus `VPS_PORT` / `APP_PORT` only if non-default.
6. **First deploy**: push to `main` (or Actions → CI/CD → Run workflow). Watch
   `journalctl -u ordora-server -f` on the VPS, then open `https://DOMAIN`.

## Day-to-day

- **Deploy**: merge to `main`. Migrations run automatically *before* the new code starts.
- **Edit contact details**: change `/opt/ordora/shared/contact.json` on the VPS —
  takes effect on next restart (`systemctl restart ordora-server`), deploys never overwrite it.
- **Logs**: `journalctl -u ordora-server -f`, `journalctl -u ordora-worker -f`.
- **Rollback**: `ln -sfn /opt/ordora/releases/<prev-sha> /opt/ordora/current &&
  systemctl restart ordora-server ordora-worker` (migrations are forward-only;
  only roll back if the deploy's migration is compatible or empty).
- **Backups**: snapshot the VPS in Truehost panel; plus `pg_dump ordora` on a cron.
  Test restores — an untested backup is not a backup.

## Troubleshooting

| Symptom | Check |
|---|---|
| Deploy fails at `curl /health` | `systemctl status ordora-server`; `journalctl -u ordora-server -n 100`; is `APP_PORT` == the port in `ordora.env`? |
| `migrate` fails | `journalctl` for the migrate step output in Actions; verify admin DB password in `ordora.env`. |
| 502 from Caddy | App not listening: `ss -ltnp \| grep 8080`; Caddyfile valid: `caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile`. |
| Port 80 busy during provision | Another proxy owns it — use its config for the domain (nginx snippet printed by provision) instead of Caddy. |
| SSH as deploy fails | `authorized_keys` perms (700/600), correct user in `VPS_USER`, key matches. |

## Notes

- `ORDORA_ENV=production` on the server: background workers run via the
  `ordora-worker` unit (the server only runs them in-process for dev).
- `contact.json` in this repo is the seed/template; the live one is on the server.
- Never commit `/etc/ordora/ordora.env` or private keys anywhere.
