# Campfire

A small blog. One binary, a folder of Markdown, and a phone app. No accounts. No database.

Ghost was too heavy. Git-managed Markdown was a pain. WordPress was worse. Prose.sh was close, but the phone side was not.

## Binary

```
go build -o campfire ./cmd/campfire
./campfire -content ./content
```

Open http://127.0.0.1:8080.

It listens on `127.0.0.1:8080`. `-content` picks the folder. `-addr` picks the address. `CAMPFIRE_CONTENT` and `CAMPFIRE_ADDR` do the same if you leave the flag off. The flag wins.

`go run` builds a throwaway binary, so pass `-content` or the files land next to that temporary file.

```
go run ./cmd/campfire -content ./content
```

`-addr :8080` listens on every interface.

## Podman or Docker

Build a Linux binary first. The image copies that file. It does not compile.

```
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o campfire ./cmd/campfire
podman build -t campfire .
podman run -d --name campfire -p 127.0.0.1:8080:8080 -v campfire-content:/content:Z campfire
```

`docker build` and `docker run` take the same arguments. Use `GOARCH=arm64` on an arm64 machine. Drop `:Z` if you are not on SELinux.

The container listens on port 8080. `-p 127.0.0.1:8080:8080` keeps it on localhost. The volume is the site. Open http://127.0.0.1:8080.

If you change the container port, repeat the flags. A new command replaces the image defaults.

```
podman run -d --name campfire -p 127.0.0.1:9090:9090 -v campfire-content:/content:Z campfire -content /content -addr :9090
```

## The folder

The first start fills an empty content directory with a few pages, styles, and templates. Setup writes `site.yaml`. Check API access if you want the phone app. That writes `.token` and shows a QR code once. On localhost the QR code uses a LAN address so the phone can reach this machine.

```
content/
  site.yaml
  index.md          home page blurb
  about.md          /about
  posts/hello.md    a post
  styles/           paper, ink, plain, nerd
  templates/        page HTML, not served
  media/            images
  .token            write API token, not served
```

`posts/*.md` is the feed, newest first. Any other `.md` file is a page. `draft: true` hides one. Drop a file in the folder and it shows up.

```markdown
---
title: Hello
date: 2026-10-09T15:04:05Z
draft: false
---

Body in **markdown**.
```

`style: paper` in `site.yaml` loads `styles/paper.css`. The others are `ink`, `plain`, and `nerd`. Edit `content/templates/` to change the HTML. A missing template file keeps the built-in page.

Replace `.token` to rotate it. Delete it and writes stop.

## API

```
curl -s -X POST http://127.0.0.1:8080/api/posts \
  -H "Authorization: Bearer $(cat content/.token)" \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello","body":"First post."}'
```

| Call | What it does |
|---|---|
| `POST /api/posts` | Create a post. 409 if that slug exists. |
| `GET /api/posts` | List posts, drafts included. |
| `GET /api/posts/{slug}` | One post, with the Markdown. |
| `PUT /api/posts/{slug}` | Replace it. The filename stays. |
| `DELETE /api/posts/{slug}` | Remove it. 204. |

Send `Authorization: Bearer` and the token. JSON fields are `title`, `slug`, `date`, `draft`, and `body`. `body` is required. `date` is RFC3339. Leave `date` off on create and Campfire uses the current time. Send the old `date` on update or the post jumps to now.
