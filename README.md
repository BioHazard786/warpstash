# ⚡ Warpstash

> **Temporary file sharing for people who don't want to sign their life away to Big Cloud just to send a 2MB screenshot.**

Warpstash is a dead-simple, self-hosted file dropper. You upload a file, get a link, send it to a friend (or enemy), and the file deletes itself when the timer runs out. No tracking, no 45-step Google Drive permission requests, and zero "Please sign up for our weekly newsletter" popups.

---

## 🧪 Live Demo

Want to kick the tires without setting anything up? Check out the live instance:

👉 **Web:** [https://stash.warplabs.info/](https://stash.warplabs.info/)  
👉 **Telegram Bot:** [@warpstash_bot](https://t.me/warpstash_bot)

> Hosted on an Oracle Cloud Free Tier VPS with a measly **50MB file limit** (shared across both web and Telegram bot) and **5GB disk space** lmao. Please don't try to stash your 4K bluray rips or the server will cry.

---

## 🎯 Why You'll Love It

- ⏳ **Self-Destructing Files**: Pick a preset or type any lifetime (`90m`, `5d`, `2w`, up to the server's cap, 30 days by default). Once time is up, the file is deleted forever. Your disk space stays clean, your past stays buried.
- 🔒 **Optional Passwords**: Lock a file behind a password. The name and size stay hidden until it's unlocked, and wrong guesses never burn a burn-after-reading file.
- 💥 **Burn After Reading**: Mission Impossible style. The second someone downloads it, it vanishes from the face of the earth. Great for configs, secrets, or embarrassing receipts.
- 🤖 **Discord & Slack Bot Shield**: You know how preview bots from Discord or Slack preview your link and accidentally trigger "burn on read"? We slap their hands away. Only actual humans downloading the file can detonate it.
- 💻 **Terminal Native (`curl` friendly)**: Don't want to leave your terminal? Pipe a command or `curl` a file straight to Warpstash and get a raw, clickable link back.
- 🐱 **Drop-in Litterbox / ShareX Support**: Already using ShareX or custom scripts built for Catbox/Litterbox? Warpstash speaks the exact same API. Zero script rewriting required.
- 🪶 **Stupidly Lightweight**: Runs as a single tiny binary with SQLite built right in. Takes almost zero RAM and boots in milliseconds.

---

## 🚀 CLI in 5 Seconds

Because opening a web browser is sometimes 3 clicks too many.

### With `curl`
```bash
# Upload a file (default: 24h expiration, returns raw URL)
curl -F "file=@screenshot.png" https://stash.warplabs.info/

# Burn after reading (self-destructs after 1 download)
curl -F "file=@secrets.env" -F "burn=true" https://stash.warplabs.info/

# Custom expiration: any mix of m, h, d, w (90m, 5d, 2w, 1d12h)
curl -F "file=@large_dump.sql" -F "time=1h" https://stash.warplabs.info/

# Password-protect a file (recipients get a password page in the browser)
curl -F "file=@payroll.xlsx" -F "password=correct-horse" https://stash.warplabs.info/

# Download a protected file from the terminal
curl -OJ -H "X-Password: correct-horse" https://stash.warplabs.info/f/a8X2mP9z.xlsx

# Pipe terminal output straight to a link
dmesg | curl -T - "https://stash.warplabs.info/upload?filename=dmesg.log&time=12h"

# Download a file (saves using server's original filename)
curl -OJ https://stash.warplabs.info/f/a8X2mP9z.png

# Download with a custom local filename
curl -o downloaded_file.png https://stash.warplabs.info/f/a8X2mP9z.png

# Delete early: ask for JSON to get the delete_url, then DELETE it
curl -H "Accept: application/json" -F "file=@notes.txt" https://stash.warplabs.info/
curl -X DELETE "<delete_url from the JSON>"
```

### With `wget` *(for the curl contrarians)*
```bash
# Upload a file (returns raw direct URL)
wget --post-file=report.pdf \
     --header="X-Filename: report.pdf" \
     --header="X-Expiry: 24h" \
     https://stash.warplabs.info/ -qO -

# Burn after reading (self-destructs after 1 download)
wget --post-file=secrets.env \
     "https://stash.warplabs.info/upload?filename=secrets.env&burn=true" -qO -

# Download a file
wget --content-disposition https://stash.warplabs.info/f/a8X2mP9z.png
```

### 💡 Shell Helpers (`~/.bashrc` or `~/.zshrc`)

Typing `curl -F "file=@..."` every time you want to send a file is tedious. Drop these quick helpers into your `~/.bashrc` or `~/.zshrc`:

```bash
# Send a file (usage: warp <file> [time] [password], time like 90m, 12h, 5d, 2w)
warp() {
    curl -F "file=@$1" ${2:+--form-string "time=$2"} ${3:+--form-string "password=$3"} https://stash.warplabs.info/
}

# Burn after reading (usage: warp-burn <file> [password])
warp-burn() {
    curl -F "file=@$1" -F "burn=true" ${2:+--form-string "password=$2"} https://stash.warplabs.info/
}

# Download a file, saved with its original name (usage: warp-get <url> [password])
warp-get() {
    curl -OJ ${2:+-H "X-Password: $2"} "$1"
}
```

Reload your shell (`source ~/.bashrc` or `source ~/.zshrc`), and you can send & receive files in one word:

```bash
# Send a file (default 24h, prints the link instantly)
warp screenshot.png

# Send with custom expiry (any of 90m, 12h, 5d, 2w...)
warp dump.sql 5d

# Lock it behind a password (time first, then the password)
warp payroll.xlsx 24h correct-horse

# Send secrets that self-destruct after 1 download
warp-burn secrets.env

# Download a file, or a password-protected one
warp-get https://stash.warplabs.info/f/a8X2mP9z.png
warp-get https://stash.warplabs.info/f/k3Zp9QmW1x.xlsx correct-horse
```

> Passwords typed on the command line end up in your shell history. Prefix the command with a space (bash/zsh with `HISTCONTROL=ignorespace`) to keep it out.

---

## 🐳 Self-Hosting

### The Easy Way (Docker Compose)

Runs the official pre-built image from GHCR. Limits come from `.env` at runtime (the web UI reads them from `/api/info`), so the same image works with any limits and no rebuild:

```bash
curl -fSL https://raw.githubusercontent.com/BioHazard786/warpstash/main/docker-compose.yml -o docker-compose.yml
curl -fSL https://raw.githubusercontent.com/BioHazard786/warpstash/main/.env.example -o .env
# Edit .env: WARPSTASH_MAX_FILE_SIZE_MB, WARPSTASH_MAX_FILES, WARPSTASH_MAX_TOTAL_STORAGE_GB, ...
docker compose up -d
```

The container binds to `127.0.0.1` only, so put Caddy or Nginx in front of it. To publish on all interfaces instead, drop the `127.0.0.1:` prefix in the compose file. It runs with a read-only filesystem and no extra privileges (everything is written to the `/data` volume), and has a healthcheck, so `docker ps` shows `healthy`.

Port 8080 taken (say, by another app)? Set `WARPSTASH_PORT=8081` in `.env`: Compose publishes that host port and the container keeps listening on 8080 internally. Point your reverse proxy at the host port.

Changed `.env` later? Run `docker compose up -d` again to apply it. To pin a release, set `WARPSTASH_VERSION=v2.0.0` in `.env` (the image tag, `latest` by default); upgrade with `docker compose pull && docker compose up -d`.

To build from source instead of pulling: `WARPSTASH_VERSION=v2.0.0-custom docker compose up -d --build`. The same value is the version shown in the app (`dev` if unset).

### The Bare-Metal Way (Single Binary)

If you prefer running binaries directly like an old-school sysadmin:

```bash
# 1. Build everything (needs Bun and Go; the SPA is embedded into the binary)
make build

# 2. Fire it up
./warpstash --port 8080 --base-url https://stash.warplabs.info
```

---

## 🌐 Reverse Proxy Setups

Put Warpstash behind your favorite web server for automatic HTTPS and domain routing.

### Option A: Caddy *(for people who value their sanity)*

Three lines of config, automatic SSL certificates, and you can go back to eating lunch:

```caddy
stash.warplabs.info {
    # Don't cut off big uploads!
    request_body {
        max_size 1GB
    }

    reverse_proxy localhost:8080
}
```

### Option B: Nginx *(for people who like semicolons)*

```nginx
server {
    listen 80;
    server_name stash.warplabs.info;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name stash.warplabs.info;

    # Your SSL certificate paths
    ssl_certificate /etc/letsencrypt/live/stash.warplabs.info/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/stash.warplabs.info/privkey.pem;

    # Allow large uploads so Nginx doesn't throw a 413 fit
    client_max_body_size 1024M;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Stream uploads directly instead of caching them to temporary disk files
        proxy_request_buffering off;
        proxy_buffering off;
    }
}
```

> **Pro-Tip**: When hosting behind Caddy or Nginx, remember to set `WARPSTASH_TRUST_PROXY=true` and `WARPSTASH_BASE_URL=https://stash.warplabs.info` so Warpstash generates proper public HTTPS links!

---

## ⚙️ Essential Configuration

You don't need a PhD in devops. Here are the only knobs you really care about:

| Setting | Default | What it does |
| :--- | :--- | :--- |
| `WARPSTASH_BASE_URL` | `http://localhost:8080` | Your public domain (used to generate the share links) |
| `WARPSTASH_PORT` | `8080` | Port the internal server listens on |
| `WARPSTASH_MAX_FILE_SIZE_MB` | `1024` (1GB) | Max upload limit per file |
| `WARPSTASH_DEFAULT_EXPIRY` | `24h` | Expiration time if the user doesn't pick one |
| `WARPSTASH_ALLOWED_EXPIRIES` | `1h,24h,7d,30d,burn` | Preset buttons in the web UI and Telegram bot. Any other duration is accepted too, up to the max below |
| `WARPSTASH_MAX_EXPIRY` | `30d` | Longest lifetime a file can be given (`90m`, `7d`, `2w`...). `0` means unlimited |
| `WARPSTASH_AUTH_TOKEN` | *(none)* | Set a password if you want this to be your private secret clubhouse |
| `WARPSTASH_TRUST_PROXY` | `false` | Flip to `true` if you're using Caddy, Nginx, or Cloudflare |
| `WARPSTASH_TELEGRAM_BOT_TOKEN` | *(none)* | Telegram bot token from @BotFather (enables embedded MTProto bot) |
| `WARPSTASH_TELEGRAM_APP_ID` | `0` | Telegram API App ID from my.telegram.org |
| `WARPSTASH_TELEGRAM_APP_HASH` | *(none)* | Telegram API App Hash from my.telegram.org |
| `WARPSTASH_TELEGRAM_ALLOWED_USERS` | *(none)* | Comma-separated allowed Telegram user IDs (empty allows all) |

---

## 🤖 Telegram MTProto Bot (Up to 2GB Files)

Try the live bot: [@warpstash_bot](https://t.me/warpstash_bot) *(shares the same 50MB limit as the live demo HTTP service).*

Warpstash includes native MTProto support for Telegram bots—embedded directly into the binary with zero HTTP API overhead:

1. Create a bot via [@BotFather](https://t.me/botfather) to get your `BOT_TOKEN`.
2. Grab your `APP_ID` and `APP_HASH` from [my.telegram.org/apps](https://my.telegram.org/apps).
3. Add them to your `.env`:
   ```env
   WARPSTASH_TELEGRAM_BOT_TOKEN=123456:ABC-DEF...
   WARPSTASH_TELEGRAM_APP_ID=12345678
   WARPSTASH_TELEGRAM_APP_HASH=abcdef0123456789...
   # Optional whitelist: comma-separated Telegram user IDs
   WARPSTASH_TELEGRAM_ALLOWED_USERS=
   ```
4. Start Warpstash (`docker compose up -d` or binary). The bot connects via MTProto, accepts large files (up to 2GB), prompts with inline retention buttons (`24h`, `72h`, `🔥 Burn`), streams directly to the server storage engine, and replies with instant shareable links. Complete panic recovery ensures bot issues never impact the web server.

---

## 📜 License

[MIT](LICENSE). Take it, self-host it, share files, let them burn.
