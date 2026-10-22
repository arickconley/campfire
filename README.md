# Campfire

A small blog. One binary and a folder of complete HTML pages. No accounts. No database.

Write with the iOS or macOS rich-text editor. The API builds each complete HTML page when saving; published post and page requests serve those files directly.

## Linux binary

Releases are at https://github.com/arickconley/campfire/releases. `campfire-linux-amd64` and `campfire-linux-arm64` are the files.

```bash
curl -L -o campfire https://github.com/arickconley/campfire/releases/latest/download/campfire-linux-amd64
chmod +x campfire
./campfire
```

Use `campfire-linux-arm64` on arm64. Open http://127.0.0.1:8080.

It listens on `127.0.0.1:8080`. The content folder is `content` next to the binary. `-content` picks another folder. `-addr` picks the address. `CAMPFIRE_CONTENT` and `CAMPFIRE_ADDR` do the same if you leave the flag off. The flag wins. `-addr :8080` listens on every interface.

The release files are Linux. To build your own:

```bash
go build -o campfire ./cmd/campfire
./campfire
```

## Podman or Docker

```bash
podman pull ghcr.io/arickconley/campfire:latest
podman run -d --name campfire -p 127.0.0.1:8080:8080 -v campfire-content:/content:Z ghcr.io/arickconley/campfire:latest
```

`docker pull` and `docker run` take the same arguments. The image is Linux amd64 and arm64. `:latest` moves. A release tag such as `:v0.1.0` stays put. Drop `:Z` if you are not on SELinux.

The container listens on port 8080. `-p 127.0.0.1:8080:8080` keeps it on localhost. The volume is the site. Open http://127.0.0.1:8080.

If you change the container port, repeat the flags. A new command replaces the image defaults.

```bash
podman run -d --name campfire -p 127.0.0.1:9090:9090 -v campfire-content:/content:Z ghcr.io/arickconley/campfire:latest -content /content -addr :9090
```

## The folder

The first start fills an empty content directory with a few pages, styles, and templates. Setup writes `site.yaml`. Check API access if you want the phone app. That writes `.token` and shows a QR code once. On localhost the QR code uses a LAN address so the phone can reach this machine.

```
content/
  site.yaml
  index.html          home page blurb
  about.html          /about
  posts/hello.html    a post
  styles/           paper, ink, plain, nerd
  templates/        page HTML, not served
  media/            images
  .token            write API token, not served
```

`posts/*.html` is the feed, newest first. Other managed `.html` documents are pages. The home page and Atom feed use the saved HTML bodies; there is no Markdown rendering or rendered-body cache during requests.

Each document stores its title, date, and draft status in the head. The marked region contains only editable post content; the template supplies the surrounding page, headings, navigation, and styles.

```html
<!doctype html>
<html lang="en">
<head>
  <title>Hello · My Blog</title>
  <meta name="campfire-title" content="Hello"/>
  <meta name="campfire-date" content="2026-10-18T14:00:00Z"/>
  <meta name="campfire-draft" content="false"/>
</head>
<body>
  <article><div data-campfire-body=""><p>Hello <strong>world</strong>.</p></div></article>
</body>
</html>
```

Drafts stay available through the authenticated API and are excluded from public pages, the home page, and the feed. Source filenames, hidden backups, tokens, and templates are not public routes. Invalid HTML metadata is never served as a static fallback.

`style: paper` in `site.yaml` loads `styles/paper.css`. The others are `ink`, `plain`, and `nerd`. Edit `content/templates/` to change the page layout. A missing template file keeps the built-in page. Post templates must include `{{.Article.HTML}}` so the editable body survives saving. After changing templates or `site.yaml`, rebuild saved pages:

```bash
./campfire -content /path/to/content -rebuild
```

Stop the server before maintenance commands. Rebuilding preserves post bodies, dates, titles, URLs, and draft status while applying the current layout. Stylesheet-only edits take effect immediately.

If an existing site's individual posts stop after three lines, update its `content/styles/*.css`: change both `article > div` selectors to `article > div:not([data-campfire-body])`. This keeps homepage excerpts clipped while showing full post bodies and their paragraph spacing. Updating the binary does not replace existing stylesheets; no page rebuild is needed for this CSS fix.

Replace `.token` to rotate it. Delete it and writes stop.

## API

```
curl -s -X POST http://127.0.0.1:8080/api/posts \
  -H "Authorization: Bearer $(cat content/.token)" \
  -H "Content-Type: application/json" \
  -d '{"format":"html","title":"Hello","body":"<p>First post.</p>"}'
```

| Call | What it does |
|---|---|
| `POST /api/posts` | Create a post. 409 if that slug exists. |
| `GET /api/posts` | List posts, drafts included. |
| `GET /api/posts/{slug}` | One post, with the editable HTML body. |
| `PUT /api/posts/{slug}` | Replace it. The filename stays. |
| `DELETE /api/posts/{slug}` | Remove it. 204. |

Send `Authorization: Bearer` and the token. JSON fields are `format`, `title`, `slug`, `date`, `draft`, and `body`. Send `format: "html"` and an HTML fragment in `body`; both are required. Responses also identify the format as `html` and include `X-Campfire-Content-Format: html`. The native apps use `application/vnd.campfire.html+json` for saves so older servers reject the request before writing an incompatible body. The server validates HTML and rejects scripts, event handlers, unsafe URLs, and unsupported elements before replacing a saved file. `date` is RFC3339. Leave `date` off on create and Campfire uses the current time. Send the old `date` on update or the post jumps to now.

## Existing Markdown sites

Update the server and both apps together. Older clients are rejected when saving because their bodies are Markdown. New clients reject incompatible server responses before editing.

With the server stopped, run:

```bash
./campfire -content /path/to/content -migrate
./campfire -content /path/to/content
```

Migration converts Markdown pages and posts to complete HTML documents and preserves every original under `.markdown-backup/` with its relative filename. Existing HTML destinations or backup files are never overwritten. A conversion error leaves the affected Markdown original intact; resolve the reported conflict before restarting. The conversion library remains solely for this explicit import command. Old `.cache/` files are unused and can be removed separately.

The editors support paragraphs, headings, emphasis, strike, links, images, lists, quotes, code, and dividers. Complex imported HTML that the editor cannot preserve is reported rather than silently discarded. Local macOS drafts from the previous Markdown editor are converted when recovered.
