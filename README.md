# PrismCat

[English](./README.md) | [简体中文](./README_CN.md)

![GitHub Release](https://img.shields.io/github/v/release/shaomian/PrismCat) ![License](https://img.shields.io/github/license/shaomian/PrismCat) ![Docker Image](https://img.shields.io/badge/image-ghcr.io%2Fshaomian%2Fprismcat-blue)

> Independently maintained fork of [PrismCat](https://github.com/paopaoandlingyia/PrismCat) — released under the same [MIT License](LICENSE).

v2.1.0 adds automatic task traces, capture runs, AI comparison reviews and task management. See the [release notes](CHANGELOG.md).

> **You never know how much junk your SDK silently injects into your prompts — until you use PrismCat.**

PrismCat is a **self-hosted, transparent proxy and debugging console for LLM APIs**.
Change one line — your `base_url` — and instantly see every request and response between your app and OpenAI / Claude / Gemini / Ollama / any LLM API, including streaming (SSE).

Go from inspecting a single request to following a complete Agent task and comparing different Agents or models, with automatic task traces, capture runs, basic metrics, AI reviews organized by selected observations, and reusable templates.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/logs-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/logs-light.png">
  <img alt="PrismCat log console: every request across every upstream, with status, tokens and latency" src="assets/logs-light.png">
</picture>

## Sound familiar?

| | PrismCat |
|---|---|
| "Why is my token usage so high? My prompt is short!" | See the hidden system prompts and few-shot examples your SDK silently injects |
| "My Agent went rogue and I have no idea what it did" | Every call is already recorded — review the full behavior chain days later |
| "I switched Agents or models — what changed on the same task?" | Compare duration, tokens, requests and errors, then review selected observations with AI and supporting logs |
| "Streaming output freezes or gets truncated" | Every SSE chunk is captured — tell whether it's the model, the gateway, or your client |
| "Function Calling keeps returning broken JSON" | Capture the raw output, tweak the prompt in the Playground, resend |
| "Multiple people share one API key — whose request failed?" | Tag requests with `X-PrismCat-Tag` and filter by user or project |
| "I run local models with Ollama and want to see the traffic" | Point an upstream at `http://localhost:11434` — it's a generic HTTP proxy |

---

## Get Started in 30 Seconds

### 1. Launch

Grab the binary for your system from [Releases](https://github.com/shaomian/PrismCat/releases).

| Platform | How to Start |
|----------|-------------|
| **Windows** | Run `prismcat.exe` — it lives in your system tray |
| **Linux / macOS** | Run `./prismcat` |
| **Docker** | See [Docker Deployment](#docker-deployment) |

Open **`http://localhost:8711`** in your browser.

### 2. Add an Upstream

In the Settings page, add an upstream. For example:

| Name | Target |
|------|--------|
| `openai` | `https://api.openai.com` |

PrismCat gives you a proxy address: **`http://openai.localhost:8711`**

### 3. Change One Line, Start Capturing

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://openai.localhost:8711/v1",  # ← change only this
    api_key="sk-..."
)

# everything else stays exactly the same
response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello!"}],
)
```

Go back to the dashboard. Your full request and response are already there. That's it.

---

## How It Works

PrismCat uses **subdomain routing** for truly transparent proxying. When you add an upstream named `openai`:

```
Your App                     PrismCat                      OpenAI
   │                           │                             │
   │  openai.localhost:8711    │   api.openai.com            │
   │ ─────────────────────────>│ ────────────────────────────>│
   │                           │       logs request ✓         │
   │<─────────────────────────│<────────────────────────────│
   │                           │       logs response ✓        │
```

**Why subdomains?** Because they make the proxy truly transparent — your request paths (like `/v1/chat/completions`) stay exactly the same. No path rewriting, no SDK quirks. Any language, any SDK, any LLM — as long as it lets you set a `base_url`, it just works. You can even chain proxies (App → PrismCat → relay → OpenAI) with zero friction.

> **💡 About `*.localhost`**: Modern browsers and most operating systems automatically resolve `*.localhost` to `127.0.0.1` — no hosts file editing required. If your environment doesn't support this, see [Path Routing Mode](#fallback-path-routing-mode) or add a hosts entry manually.

---

## Key Features

### Full Traffic Observability
- Complete request/response headers and bodies with keyword search and highlighting
- **SSE streaming** captured in full — view raw chunks or the merged result
- **Responses WebSocket** records individual model calls and response events for timeline views, usage extraction and analysis
- Auto-formatted JSON, smart Base64 folding (no more drowning in image data) with one-click image preview
- Copy HTTP requests as ready-to-run **cURL commands**

![Image Preview](assets/image_preview.png)


### One-Click Replay (Playground)
See a failed HTTP request? Hit **Replay**, tweak the prompt or parameters right in your browser, and resend instantly. No need to re-run your Python/Node script. HTTP replay and cURL export are not currently available for WebSocket calls.

### Automatic Task Traces & Capture Runs
- Infer task membership from protocol references, tool calls and context without custom client trace headers; automatic correlation is off by default and explicit trace IDs take priority
- Inspect conversation timelines, request relationship diagrams, correlation evidence and predecessors, with manual corrections when needed
- Start and stop a capture run for a proxy endpoint to collect its logged requests during a task; ordinary capture runs work without automatic correlation

Supports OpenAI Chat Completions and Responses, Anthropic Messages, and Gemini over HTTP/SSE, plus Responses WebSocket. Automatic correlation is an inference and remains uncertain when evidence is insufficient. See [Automatic traces](AUTO_TRACE.md).

### Agent / Model Comparisons & AI Task Reviews
- Add existing traces or capture runs to a review card and compare **duration, tokens, request counts and error counts**; viewing basic metrics does not call AI
- Choose tasks, select observations, then review the results: select 2–8 tasks, write your own questions, or ask AI for candidate observations and confirm the ones you want
- Start with code development, penetration testing, reverse analysis or free-form templates, and save your own observations and analysis instructions for other scenarios
- Click to let AI inspect logs and produce a report with supporting logs, inference labels and improvement suggestions; open source logs, revisit saved reports and ask follow-up questions

Trace analysis and comparison reviews share model settings, supporting OpenAI-compatible APIs and native Anthropic Messages. Sync a model list or enter a model ID manually. The review model is independent of the tested Agent's model, with its usage shown separately. See the [comparison and AI review guide (Chinese)](EXPERIMENTS_CN.md).

### Usage Tracking
Extract token usage from responses with built-in OpenAI, Anthropic and Gemini rules, or define your own. Enable Usage extraction and bind rules to the relevant upstream. Missing usage remains unknown rather than zero, and enabling extraction does not automatically recalculate historical logs.

### Task Management & Cleanup
Archive, restore, delete and batch-manage traces, stopped capture runs, review cards and custom templates. Preview deletion scope with reference and activity checks, or manually clean expired logs while preserving retention protections such as favorites and review references. Archiving only hides items and does not reclaim storage. See [Task management and cleanup](TASK_MANAGEMENT.md).

### Target Presets
One stable proxy endpoint, several complete targets behind it — switch between them from Settings without touching the client.

- Each preset bundles target URL, timeouts, outbound proxy, and rule bindings, so you can't end up with a new URL and an old key
- Useful for production/staging, direct/proxied routes, regional endpoints, tenant credentials, and live-vs-mock servers
- A switch affects new requests only; in-flight streams finish against the target they started with

### Request Override (Opt-In)
Rewrite outbound requests without touching your code — set, remove, or conditionally default JSON body fields, append/prepend to arrays, and set or strip HTTP headers. Each rule is matched by method / path / JSON content; the log detail page shows a side-by-side diff of the original vs. final request.

> **🔒 Strictly opt-in.** PrismCat is a transparent proxy by default and **never** touches your requests unless you (1) flip the master switch, (2) define rules, and (3) bind them to specific upstreams. Skip any of those steps and every byte is forwarded untouched.

### Privacy & Security
- **Self-hosted** — logs are stored in local SQLite + filesystem
- AI analysis and reviews run only when clicked and send the required logs and supplementary material to your configured model service
- Automatic masking of sensitive headers (`Authorization`, `api-key`)

### Log Tagging
Add `X-PrismCat-Tag: my-tag` to any request header to categorize logs in the UI. Perfect for shared proxies with multiple users or projects.

### Dead-Simple Deployment
Single binary, zero dependencies. Windows system tray support. Native Docker image available.

### Always-On, Always Reviewable
Built to run as a silent, 24/7 black box — you never have to "remember to start capturing."

- Automatic retention cleanup and large-body offloading keep storage healthy over months
- Review what an autonomous Agent actually sent and received, days after the fact

---

## PrismCat vs. Alternatives

| | PrismCat | mitmproxy | Langfuse / Helicone |
|---|---------|-----------|---------------------|
| Deployment | Single binary / Docker | Local install + certs | SaaS or complex self-host |
| LLM-Optimized | ✅ JSON formatting, Base64 folding, SSE merge | ❌ Generic HTTP inspector | ✅ But geared toward production monitoring |
| One-Click Replay | ✅ Built-in Playground | ❌ | Partial |
| Integration | Change `base_url` | System-wide proxy / certs | Instrument SDK code |
| Data Ownership | Fully local | Fully local | Third-party dependent |
| Stream Playback | ✅ Raw + merged view | Poor UX | Partial |
| Long-Term Running | ✅ Auto-cleanup, silent background | Ad-hoc debugging tool | ✅ But requires external infra |

---

## Build and Run from Source

Install Go (see `go.mod` for the required version) and Node.js (24 recommended), then use an entry point in the repository root:

| Platform | Entry point |
|----------|-------------|
| macOS | Double-click `快速编译并运行.command`, or run `bash ./快速编译并运行.command` |
| Windows | Double-click `快速编译并运行.bat` |
| Shared terminal entry | `node scripts/build-and-run.mjs` |

Both launchers use `scripts/build-and-run.mjs`: run `npm ci` when dependencies change, build the frontend, sync embedded assets while preserving `internal/server/ui/README.md`, and compile the backend. Only after all build steps succeed does the script stop the old instance and replace the root executable. Failed builds leave the old instance running. On macOS, only the PID-file instance with a matching executable path is restarted; Windows only stops `prismcat.exe` instances whose executable path matches this checkout.

The working directory is always the repository root, using `data/config.yaml` and `data/` by default. macOS keeps logs in the terminal and stops with Ctrl+C; Windows launches the tray app, which can be closed from its tray menu. Backend arguments are forwarded:

```sh
node scripts/build-and-run.mjs -config "data/my config.yaml"
```

To verify the build without stopping or replacing the running executable:

```sh
node scripts/build-and-run.mjs --build-only
```

The build-only executable is saved under `.cache/local-debug/build-*/`; the command prints its full path. This mode still updates local frontend and embedded assets. The root Windows `start-prismcat.bat` and `restart-prismcat.bat` scripts start or restart an existing executable without rebuilding.

---

## Docker Deployment

### Docker Compose

Create a `docker-compose.yml`:

```yaml
services:
  prismcat:
    image: ghcr.io/shaomian/prismcat:latest
    container_name: prismcat
    ports:
      - "8711:8711"
    environment:
      # Dashboard hosts. Use localhost locally; use your domain or IP on a server.
      - PRISMCAT_UI_HOSTS=localhost,127.0.0.1
      # Base domain for subdomain routing. For bare-IP deployments, enable path routing instead.
      - PRISMCAT_PROXY_DOMAINS=localhost
      # For bare IP / no wildcard domain deployments: set PRISMCAT_UI_HOSTS to your IP and enable path routing.
      # - PRISMCAT_UI_HOSTS=YOUR_IP
      # - PRISMCAT_ENABLE_PATH_ROUTING=true
      # Recommended for public-facing deployments; leave empty to set it on first UI access
      - PRISMCAT_UI_PASSWORD=your_strong_password
      - PRISMCAT_RETENTION_DAYS=30
    volumes:
      - ./data:/app/data
    restart: always
```

```bash
docker compose up -d
```

### Docker Run

```bash
docker run -d --name prismcat \
  -p 8711:8711 \
  -e PRISMCAT_UI_HOSTS=localhost,127.0.0.1 \
  -e PRISMCAT_PROXY_DOMAINS=localhost \
  -e PRISMCAT_UI_PASSWORD=your_strong_password \
  -e PRISMCAT_RETENTION_DAYS=30 \
  -v ./data:/app/data \
  --restart always \
  ghcr.io/shaomian/prismcat:latest
```

---

## Fallback: Path Routing Mode

If your environment can't resolve `*.localhost`, or you're deploying to a bare IP without a wildcard domain, enable **path routing mode** in Settings to route by URL path instead of subdomain:

```python
# Path routing mode — no subdomain resolution needed
client = OpenAI(
    base_url="http://localhost:8711/_proxy/openai/v1",  # On a server: http://YOUR_IP:8711/_proxy/openai/v1
    api_key="sk-..."
)
```

Enable via config or environment variable:

```yaml
# config.yaml
server:
  enable_path_routing: true
  path_routing_prefix: "/_proxy"
```

```bash
# or via environment variable
PRISMCAT_ENABLE_PATH_ROUTING=true
```

> **Note**: Path routing adds a prefix to your request URL (e.g., `/_proxy/openai/...`), which may require extra care with how some SDKs construct paths. Subdomain mode doesn't have this caveat.

---

## Production Deployment (Nginx + Wildcard Domain)

For public-facing deployments, use a wildcard domain (e.g., `*.prismcat.example.com`) with Nginx:

```nginx
server {
    listen 80;
    server_name prismcat.example.com *.prismcat.example.com;

    location / {
        proxy_pass http://127.0.0.1:8711;
        proxy_set_header Host $host;  # Required: pass original Host for subdomain routing

        # Required for SSE / streaming
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_buffering off;

        client_max_body_size 50M;
    }
}
```

Then add `prismcat.example.com` to PrismCat's `proxy_domains`. The dashboard is available at `prismcat.example.com`, and your upstream `openai` is available at `openai.prismcat.example.com`.

---

## Configuration Reference

The config file lives at `data/config.yaml` and is created on first launch. Most settings can also be changed from the Settings page in the UI.

<details>
<summary>Full config example</summary>

```yaml
server:
  port: 8711
  ui_password: ""           # Console password; leave empty to set it on first UI access
  proxy_domains:            # Base domains for subdomain routing
    - localhost

logging:
  max_request_body: 5242880       # Save request content up to 5MB
  max_response_body: 33554432     # Save response content up to 32MB
  sensitive_headers:              # Headers to auto-mask
    - Authorization
    - api-key
    - x-api-key
  detach_body_over_bytes: 2097152 # Load bodies > 2MB on demand
  body_preview_bytes: 524288      # Inline readable preview; lower for high-frequency long-running use
  early_request_body_snapshot: false

logging_rules:
  # Filled once from the embedded ai_models.json catalog, then fully user-managed.
  model_path_templates_initialized: false
  model_path_templates: []

storage:
  retention_days: 30              # Log retention in days; 0 = keep forever

upstreams:
  openai:
    target: "https://api.openai.com"
    timeout: 300 # Total timeout for the entire upstream request and response; new configs default to 5 minutes
    response_header_timeout: 60 # Advanced: wait for HTTP status/headers; 0 disables; always bounded by total timeout
    response_body_first_byte_timeout: 30 # Advanced: wait for the first response body byte after headers; applies to all responses
    response_body_idle_timeout: 15 # Advanced: maximum silence after the body starts; resets whenever data arrives
    sse_keepalive: # Optional; off by default, and heartbeats never extend the total timeout
      mode: off # off, after_headers, or full (including the response-header wait)
      interval: 15 # Upstream silence before PrismCat sends a downstream SSE comment
    outbound_proxy: "env"          # env, direct, or a proxy URL such as http://127.0.0.1:7890
    logging_path_filter:            # all, allowlist, or denylist; Ant and RE2 regex rules can be mixed
      mode: allowlist
      rules:
        - matcher: ant
          pattern: "/v1/responses"
        - matcher: regex
          pattern: "^/v1/chat/completions$"
  gemini:
    target: "https://generativelanguage.googleapis.com"
    timeout: 300
    response_header_timeout: 0
    response_body_first_byte_timeout: 0
    response_body_idle_timeout: 0 # 0 disables the corresponding stage timeout
    outbound_proxy: "http://127.0.0.1:7890"
  # Optional: multiple complete target presets behind one stable endpoint.
  # target and targets are mutually exclusive; the legacy single-target form remains supported.
  codex:
    active_target: primary
    targets:
      primary:
        url: "https://api.openai.com"
        timeout: 300
        outbound_proxy: "env"
        request_overrides:
          enabled: false
          rule_names: []
      backup:
        url: "https://api.example.com"
        timeout: 300
        outbound_proxy: "direct"
        request_overrides:
          enabled: false
          rule_names: []

`after_headers` injects `: ping` comments only after a successful, uncompressed `text/event-stream` response is confirmed. `full` also covers the response-header wait when PrismCat recognizes `Accept: text/event-stream`, `alt=sse`, or a top-level JSON `stream: true`. Once the first early heartbeat commits HTTP 200, a later upstream 401, 429, or 5xx can only be recorded in PrismCat logs and the downstream observes a terminated stream. Explicit `timeout` values in existing configurations are not migrated when the default changes.

# Request override (opt-in, off by default)
# Supports ops: set, remove, default, append, prepend for JSON body; set, remove for headers.
request_overrides:
  enabled: false
  max_body_bytes: 1048576
  upstreams: {}
  rules: []

# Token usage extraction (off by default)
# Built-in rules for OpenAI, Anthropic, Gemini; define your own via paths.
usage_extraction:
  enabled: false
  upstreams: {}
  rules: []   # see config.example.yaml for built-in rule definitions
```

</details>

---

## FAQ

<details>
<summary><b>Q: <code>openai.localhost</code> doesn't work?</b></summary>

Most modern systems resolve `*.localhost` to `127.0.0.1` automatically. If yours doesn't:
1. Add `127.0.0.1 openai.localhost` to your hosts file
2. Or enable [Path Routing Mode](#fallback-path-routing-mode) as a workaround
3. Or use your own wildcard domain (see [Nginx Deployment](#production-deployment-nginx--wildcard-domain))
</details>

<details>
<summary><b>Q: Streaming feels "stuck"?</b></summary>

If you're behind a reverse proxy (e.g., Nginx), make sure you have:
- `proxy_buffering off;`
- `proxy_http_version 1.1;`

Nginx buffers entire responses by default, making streaming look like it's hanging.
</details>

<details>
<summary><b>Q: Which LLM services are supported?</b></summary>

PrismCat is a generic HTTP proxy — it's not tied to any specific LLM provider. Any HTTP/HTTPS API works, including:
- OpenAI / Azure OpenAI
- Anthropic Claude
- Google Gemini
- Ollama / LM Studio (local models)
- API relay services / aggregators
</details>

<details>
<summary><b>Q: Does it add latency?</b></summary>

PrismCat uses asynchronous log writing. The proxy overhead is typically under 1ms. Logging never blocks request forwarding.
</details>

---

## Acknowledgments

Thanks to the [original PrismCat project](https://github.com/paopaoandlingyia/PrismCat) for its open-source contribution.
