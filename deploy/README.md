# Deploying to EC2

This deploys Postgres + the Go backend + a Caddy reverse proxy (automatic
HTTPS) as three Docker containers on a single EC2 instance. No container
registry needed — the backend image is built on the instance itself from
source, same as local dev.

## 1. Launch the instance

- Ubuntu 24.04 LTS, `t3.small` is plenty for a single hostel (bump up if
  serving many hostels).
- Security group: allow inbound **22** (SSH, ideally restricted to your
  IP), **80** and **443** (HTTP/HTTPS, from anywhere — Caddy needs 80 open
  to complete the Let's Encrypt HTTP-01 challenge, then redirects to 443).
  Do **not** open 5432 or 8080 — Postgres and the backend are only reachable
  from inside the Docker network, not from the internet, by design (see
  `docker-compose.prod.yml`).
- Attach enough EBS storage for your Postgres data (20GB is generous for
  this workload).

## 2. Point a domain at it

Create an A record for your domain (e.g. `mess.yourdomain.com`) pointing at
the instance's public IP. Caddy's automatic HTTPS needs a real domain — a
bare IP address won't get a certificate.

## 3. Install Docker

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER
# log out and back in for the group change to take effect
```

This installs the Docker Compose plugin too (`docker compose`, not the
older standalone `docker-compose`).

## 4. Get the code onto the instance

Either `git clone` your repo, or upload this zip and unzip it. Either way,
end up with this `deploy/` folder present on the instance.

## 5. Configure secrets

```bash
cd ymca-mess/deploy
cp .env.prod.example .env.prod
nano .env.prod   # fill in a real POSTGRES_PASSWORD and your DOMAIN
chmod +x deploy.sh
```

## 6. Deploy

```bash
./deploy.sh
```

First run takes a few minutes (building the Go image, Caddy requesting its
certificate). Check status any time:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod ps
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f backend
```

Visit `https://<your-domain>/healthz` — should return `{"status":"ok"}`.

## 7. Point the mobile app at it

In `mobile/composeApp/src/commonMain/kotlin/com/ymca/mess/App.kt`, construct
`ApiClient` with your real URL:

```kotlin
val api = remember { ApiClient(sessionStore, baseUrl = "https://mess.yourdomain.com") }
```

## 8. Before onboarding real members: swap the OTP sender

`cmd/api/main.go` currently wires `auth.ConsoleSender`, which logs OTP
codes to the container's stdout instead of sending real email/SMS — fine
for testing this deployment, not fine for real members who can't see your
server logs. Implement `app.OTPSender` (two methods: `SendEmail`,
`SendSMS`) against a real provider (SES, SNS, Twilio, etc.), swap the one
line in `main.go`, redeploy.

## Redeploying after code changes

```bash
git pull   # or re-upload
./deploy.sh
```

## Backups

Postgres data lives in the `pg_data` named volume. At minimum, cron a
periodic `pg_dump`:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod exec -T postgres \
  pg_dump -U mess ymca_mess > "backup-$(date +%F).sql"
```
