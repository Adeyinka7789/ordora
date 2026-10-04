#!/usr/bin/env bash
# scripts/provision-vps.sh — one-shot production prep for an Ubuntu VPS
# (e.g. Truehost) that already hosts other apps. Safe to re-run: every step
# is idempotent and nothing belonging to other apps is touched.
#
# Usage (as root on the VPS):
#   DOMAIN=ordora.example.com APP_PORT=8080 bash provision-vps.sh
#
# What it does:
#   1. Installs PostgreSQL 16 (only if missing) and creates the ordora
#      database + ordora_app / ordora_admin roles + extensions + grants.
#   2. Creates `ordora` (app runtime) and `deploy` (CI SSH) users, dirs,
#      /etc/ordora/ordora.env with generated secrets, shared contact.json.
#   3. Installs systemd units (enabled, not started) for server + worker.
#   4. Installs Caddy (only if nothing already listens on 80/443) and adds
#      an auto-HTTPS site for DOMAIN -> 127.0.0.1:APP_PORT.
#   5. Prints the GitHub secrets + remaining manual steps.
set -euo pipefail

DOMAIN="${DOMAIN:?Set DOMAIN, e.g. DOMAIN=ordora.example.com}"
APP_PORT="${APP_PORT:-8080}"
DB_NAME="${DB_NAME:-ordora}"
APP_DB_USER="ordora_app"
ADMIN_DB_USER="ordora_admin"

echo "==> Provisioning Ordora for ${DOMAIN} (app port ${APP_PORT}, db ${DB_NAME})"

# ---------------------------------------------------------------- ports ---
if command -v ss >/dev/null 2>&1; then
  if ss -ltn 2>/dev/null | grep -Eq ':80\b'; then
    echo "!! Something already listens on port 80. Caddy install skipped."
    echo "   Proxy ${DOMAIN} to 127.0.0.1:${APP_PORT} in your existing proxy instead."
    echo "   An nginx snippet is printed at the end of this script's output."
    SKIP_PROXY=1
  fi
fi
SKIP_PROXY="${SKIP_PROXY:-0}"

# ------------------------------------------------------------- packages ---
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
if ! command -v psql >/dev/null 2>&1; then
  echo "==> Installing PostgreSQL 16"
  apt-get install -y postgresql-16
else
  echo "==> PostgreSQL already present, leaving it alone"
fi
if ! command -v curl >/dev/null 2>&1; then apt-get install -y curl; fi
if ! command -v openssl >/dev/null 2>&1; then apt-get install -y openssl; fi

# ------------------------------------------------------------- database ---
echo "==> Ensuring database objects"
sudo -u postgres psql -v ON_ERROR_STOP=1 <<SQL
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '${APP_DB_USER}') THEN
    CREATE ROLE ${APP_DB_USER} WITH LOGIN NOBYPASSRLS;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '${ADMIN_DB_USER}') THEN
    CREATE ROLE ${ADMIN_DB_USER} WITH LOGIN BYPASSRLS;
  END IF;
END
\$\$;
SELECT 'CREATE DATABASE ${DB_NAME}' WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = '${DB_NAME}')\gexec
SQL
APP_DB_PASSWORD="$(openssl rand -hex 18)"
ADMIN_DB_PASSWORD="$(openssl rand -hex 18)"
sudo -u postgres psql -v ON_ERROR_STOP=1 -d "${DB_NAME}" <<SQL
ALTER ROLE ${APP_DB_USER} WITH PASSWORD '${APP_DB_PASSWORD}';
ALTER ROLE ${ADMIN_DB_USER} WITH PASSWORD '${ADMIN_DB_PASSWORD}';
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
GRANT CONNECT ON DATABASE ${DB_NAME} TO ${APP_DB_USER}, ${ADMIN_DB_USER};
GRANT USAGE ON SCHEMA public TO ${APP_DB_USER}, ${ADMIN_DB_USER};
ALTER DEFAULT PRIVILEGES FOR ROLE ${ADMIN_DB_USER} IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ${APP_DB_USER};
ALTER DEFAULT PRIVILEGES FOR ROLE ${ADMIN_DB_USER} IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO ${APP_DB_USER};
SQL

# ---------------------------------------------------------------- users ---
id -u ordora >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin ordora
id -u deploy >/dev/null 2>&1 || { useradd --create-home --shell /bin/bash deploy; }
mkdir -p /home/deploy/.ssh && chmod 700 /home/deploy/.ssh
touch /home/deploy/.ssh/authorized_keys && chmod 600 /home/deploy/.ssh/authorized_keys
chown -R deploy:deploy /home/deploy/.ssh
cat >/etc/sudoers.d/deploy-ordora <<'SUDO'
deploy ALL=(ALL) NOPASSWD: /bin/systemctl restart ordora-server, /bin/systemctl restart ordora-worker, /bin/systemctl status ordora-server, /bin/systemctl status ordora-worker
SUDO
chmod 440 /etc/sudoers.d/deploy-ordora

# ----------------------------------------------------------------- dirs ---
mkdir -p /opt/ordora/releases /opt/ordora/shared/uploads /etc/ordora
chown -R ordora:ordora /opt/ordora
chmod 755 /opt/ordora /opt/ordora/releases /opt/ordora/shared

# ------------------------------------------------------------------ env ---
if [ ! -f /etc/ordora/ordora.env ]; then
  cat >/etc/ordora/ordora.env <<ENV
ORDORA_ENV=production
ORDORA_BASE_URL=https://${DOMAIN}
ORDORA_HTTP_ADDR=:${APP_PORT}
ORDORA_DB_HOST=localhost
ORDORA_DB_PORT=5432
ORDORA_DB_NAME=${DB_NAME}
ORDORA_DB_USER=${APP_DB_USER}
ORDORA_DB_PASSWORD=${APP_DB_PASSWORD}
ORDORA_DB_SSLMODE=disable
ORDORA_DB_ADMIN_USER=${ADMIN_DB_USER}
ORDORA_DB_ADMIN_PASSWORD=${ADMIN_DB_PASSWORD}
ORDORA_SESSION_COOKIE_NAME=ordora_session
ORDORA_CSRF_COOKIE_NAME=ordora_csrf
ORDORA_SESSION_TTL_HOURS=720
ORDORA_SESSION_IDLE_HOURS=168
ORDORA_EMAIL_MODE=console
ORDORA_EMAIL_FROM=no-reply@${DOMAIN}
ORDORA_STORAGE_MODE=local
ORDORA_STORAGE_LOCAL_DIR=/opt/ordora/shared/uploads
ORDORA_SUPPORT_EMAIL=support@${DOMAIN}
ORDORA_CONTACT_FILE=/opt/ordora/shared/contact.json
# TODO: set a long random path, keep it secret
ORDORA_ADMIN_PATH=/ops-change-me-please
ORDORA_ADMIN_SESSION_COOKIE=ordora_admin_session
ORDORA_ADMIN_SESSION_TTL_HOURS=8
ENV
  echo "==> Wrote /etc/ordora/ordora.env (edit SMTP + ADMIN_PATH!)"
else
  echo "==> /etc/ordora/ordora.env exists, leaving it (DB passwords unchanged)"
  echo "    This run's generated passwords were NOT applied. To rotate, edit the file."
fi
chown root:ordora /etc/ordora/ordora.env
chmod 640 /etc/ordora/ordora.env

# -------------------------------------------------------------- contact ---
if [ ! -f /opt/ordora/shared/contact.json ]; then
  cat >/opt/ordora/shared/contact.json <<'JSON'
{
  "emails": ["hello@example.com"],
  "phones": ["+234 800 000 0000"],
  "address": "Lagos, Nigeria",
  "website": "example.com",
  "website_url": "https://example.com",
  "whatsapp_number": "+234 800 000 0000",
  "whatsapp_message": "Hello! I found you through your website and I have a question."
}
JSON
  chown ordora:ordora /opt/ordora/shared/contact.json
  echo "==> Seeded /opt/ordora/shared/contact.json (edit with real details)"
fi

# ---------------------------------------------------------------- systemd -
cat >/etc/systemd/system/ordora-server.service <<'UNIT'
[Unit]
Description=Ordora web server
After=network.target postgresql.service
Wants=postgresql.service

[Service]
User=ordora
Group=ordora
WorkingDirectory=/opt/ordora/current
EnvironmentFile=/etc/ordora/ordora.env
ExecStart=/opt/ordora/current/server
Restart=always
RestartSec=5
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
cat >/etc/systemd/system/ordora-worker.service <<'UNIT'
[Unit]
Description=Ordora background worker
After=network.target postgresql.service
Wants=postgresql.service

[Service]
User=ordora
Group=ordora
WorkingDirectory=/opt/ordora/current
EnvironmentFile=/etc/ordora/ordora.env
ExecStart=/opt/ordora/current/worker
Restart=always
RestartSec=5
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable ordora-server ordora-worker >/dev/null
echo "==> systemd units installed + enabled (first deploy will start them)"

# ------------------------------------------------------------------ proxy -
if [ "${SKIP_PROXY}" = "0" ]; then
  if ! command -v caddy >/dev/null 2>&1; then
    echo "==> Installing Caddy"
    apt-get install -y debian-keyring debian-archive-keyring apt-transport-https
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
    apt-get update -y && apt-get install -y caddy
  fi
  mkdir -p /etc/caddy/sites-enabled
  cat >/etc/caddy/sites-enabled/ordora <<CADDY
${DOMAIN} {
	reverse_proxy 127.0.0.1:${APP_PORT}
}
CADDY
  if ! grep -q "sites-enabled" /etc/caddy/Caddyfile 2>/dev/null; then
    printf '\nimport /etc/caddy/sites-enabled/*\n' >>/etc/caddy/Caddyfile
  fi
  caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
  systemctl enable --now caddy
  systemctl reload caddy
  echo "==> Caddy site added for ${DOMAIN} (auto-HTTPS on first request)"
else
  echo "==> Skipped Caddy (port 80 busy). Nginx snippet for ${DOMAIN}:"
  echo "    server { listen 80; server_name ${DOMAIN};"
  echo "      location / { proxy_pass http://127.0.0.1:${APP_PORT};"
  echo "        proxy_set_header Host \$host; proxy_set_header X-Forwarded-Proto \$scheme; } }"
fi

# ---------------------------------------------------------------- firewall -
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
  ufw allow OpenSSH >/dev/null; ufw allow 80,443/tcp >/dev/null
  echo "==> UFW rules ensured (80/443/ssh)"
fi

cat <<NEXT

================= DONE — next steps =================
1. DNS: point ${DOMAIN} (A record) at this VPS IP.
2. Fill secrets on the server: edit /etc/ordora/ordora.env
   (SMTP settings, ORDORA_ADMIN_PATH) and /opt/ordora/shared/contact.json.
3. Add the deploy key: paste your GitHub Actions public key into
   /home/deploy/.ssh/authorized_keys
4. GitHub → Settings → Secrets → Actions:
     VPS_HOST=${DOMAIN} (or VPS IP)
     VPS_USER=deploy
     VPS_SSH_KEY=<private key matching authorized_keys>
     VPS_PORT=22            (only if SSH is not on 22)
     APP_PORT=${APP_PORT}          (only if different)
   Then push to main (or Run workflow → Deploy) for the first deploy.
5. Watch it: Actions tab → Deploy → journal on VPS:
     journalctl -u ordora-server -f
Rollback: ln -sfn /opt/ordora/releases/<prev> /opt/ordora/current &&
  systemctl restart ordora-server ordora-worker
=====================================================
NEXT
