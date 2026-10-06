# ⚡ Warpstash

> **Temporary file sharing for people who don't want to sign their life away to Big Cloud just to send a 2MB screenshot.**

Warpstash is a dead-simple, self-hosted file dropper. You upload a file, get a link, send it to a friend (or enemy), and the file deletes itself when the timer runs out. No tracking, no 45-step Google Drive permission requests, and zero "Please sign up for our weekly newsletter" popups.

---

## 🎯 Why You'll Love It

- ⏳ **Self-Destructing Files**: Pick an expiration time (`1h`, `12h`, `24h`, `72h`). Once time is up, the file is deleted forever. Your disk space stays clean, your past stays buried.
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
curl -F "file=@screenshot.png" https://files.example.com/

# Burn after reading (self-destructs after 1 download)
curl -F "file=@secrets.env" -F "burn=true" https://files.example.com/

# Custom expiration (1h, 12h, 24h, 72h)
curl -F "file=@large_dump.sql" -F "time=1h" https://files.example.com/

# Pipe terminal output straight to a link
dmesg | curl -T - "https://files.example.com/upload?filename=dmesg.log&time=12h"
```

### With `wget` *(for the curl contrarians)*
```bash
# Upload a file (returns raw direct URL)
wget --post-file=report.pdf \
     --header="X-Filename: report.pdf" \
     --header="X-Expiry: 24h" \
     https://files.example.com/upload -qO -

# Burn after reading (self-destructs after 1 download)
wget --post-file=secrets.env \
     "https://files.example.com/upload?filename=secrets.env&burn=true" -qO -
```

---

## 🐳 Self-Hosting

### The Easy Way (Docker Compose)

Using the included [docker-compose.yml](docker-compose.yml):

```bash
# 1. Clone & enter
git clone https://github.com/your-username/warpstash.git
cd warpstash

# 2. Set your domain and settings
cp .env.example .env

# 3. Build and start
docker compose up -d --build
```

### The Bare-Metal Way (Single Binary)

If you prefer running binaries directly like an old-school sysadmin:

```bash
# 1. Build everything (frontend + standalone Go binary)
make build

# 2. Fire it up
./warpstash --port 8080 --base-url https://files.example.com
```

---

## 🌐 Reverse Proxy Setups

Put Warpstash behind your favorite web server for automatic HTTPS and domain routing.

### Option A: Caddy *(for people who value their sanity)*

Three lines of config, automatic SSL certificates, and you can go back to eating lunch:

```caddy
files.example.com {
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
    server_name files.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name files.example.com;

    # Your SSL certificate paths
    ssl_certificate /etc/letsencrypt/live/files.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/files.example.com/privkey.pem;

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

> **Pro-Tip**: When hosting behind Caddy or Nginx, remember to set `WARPSTASH_TRUST_PROXY=true` and `WARPSTASH_BASE_URL=https://files.example.com` so Warpstash generates proper public HTTPS links!

---

## ⚙️ Essential Configuration

You don't need a PhD in devops. Here are the only knobs you really care about:

| Setting | Default | What it does |
| :--- | :--- | :--- |
| `WARPSTASH_BASE_URL` | `http://localhost:8080` | Your public domain (used to generate the share links) |
| `WARPSTASH_PORT` | `8080` | Port the internal server listens on |
| `WARPSTASH_MAX_FILE_SIZE_MB` | `1024` (1GB) | Max upload limit per file |
| `WARPSTASH_DEFAULT_EXPIRY` | `24h` | Expiration time if the user doesn't pick one (`1h`, `12h`, `24h`, `72h`) |
| `WARPSTASH_AUTH_TOKEN` | *(none)* | Set a password if you want this to be your private secret clubhouse |
| `WARPSTASH_TRUST_PROXY` | `false` | Flip to `true` if you're using Caddy, Nginx, or Cloudflare |

---

## 📜 License

[MIT](LICENSE). Take it, self-host it, share files, let them burn.
