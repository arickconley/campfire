# Campfire

## Why this exists

I wanted an easy way to publish posts from my phone to a very simple blog. Ghost was too heavy. Git-managed Markdown was too clunky. WordPress, don't even go there.

It had to be extremely simple. Run one app. No accounts. No complex layouts. Words on the screen.

I liked Prose.sh, but I wanted a better experience on a phone. So I made Campfire. One Go binary, simple templates, no web editor, and a decent mobile app. Posts are Markdown, so git and a desktop editor still work. A small REST API is there if you want to write your own client.

## Run

```
go run ./cmd/campfire
```

Open http://localhost:8080

With no flags, the content directory is `content` next to the binary, and Campfire listens on `127.0.0.1:8080`. `go run` builds a temporary binary, so that content directory is next to the temporary file. Pass `-content` when you want a fixed folder.

```
campfire -content ./content -addr 127.0.0.1:8080
```

Env: `CAMPFIRE_CONTENT`, `CAMPFIRE_ADDR`. A flag wins when you pass it. Reads stay public.

The first start creates the content directory when that path is missing or has no visible files. The starter tree is the pages, styles, and templates below. `site.yaml` is not part of it. The setup page writes that file. An existing file in the directory is left as it is.

If `site.yaml` is missing, Campfire serves a setup page until the form is submitted. Submitting the form writes `site.yaml`. API access is off unless that box is checked. Checking it writes `content/.token` and then shows the token and the iOS setup QR. The code uses a LAN address when you open the page on localhost. After that, the blog is served. The token file is not served. A missing file disables the write API. Replace the file to rotate the token.

## Folder

```
content/
  site.yaml
  index.md                 blurb above the feed
  about.md                 /about
  styles/
    paper.css              cool mist
    ink.css                dark
    plain.css              system font
    nerd.css               green phosphor terminal
  templates/               page HTML, not served
  posts/
    hello.md               /posts/hello and the home feed
  media/
    mark.svg               /media/mark.svg
  .token                   write API bearer token, not served
```

`posts/*.md` is the feed, newest first. Any other `.md` file is a page at its path. `index.md` in a folder is that folder's URL. `content/index.md` is the home blurb. `draft: true` hides a file from HTML and `/feed.xml`. Dotfiles, `_` names, and `site.yaml` are not served. Rendered HTML is one file per post in `.cache/`. An edit replaces that file. It is not kept in memory and it is not served.

```markdown
---
title: Hello
date: 2026-10-09T15:04:05Z
draft: false
---

Body in **markdown**.
```

## Publish

```
go run ./cmd/campfire

curl -s -X POST localhost:8080/api/posts \
  -H "Authorization: Bearer $(cat content/.token)" \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello","body":"First post."}'
```

| Call | Result |
|---|---|
| `POST /api/posts` | Create `content/posts/<slug>.md`. 409 if the slug exists. |
| `GET /api/posts` | List posts, including drafts. |
| `GET /api/posts/{slug}` | One post, Markdown included. |
| `PUT /api/posts/{slug}` | Replace the body and front matter. The filename stays. |
| `DELETE /api/posts/{slug}` | Remove the file. 204. |

Send `Authorization: Bearer` and the token from `.token`. JSON fields: `title`, `slug`, `date` (RFC3339), `draft`, `body`. `body` is required. An empty slug comes from the title, then from the time. The filename is the slug. The posted date is `date` in the front matter.

Dropping a `.md` file in the folder publishes it the same way.

## Style

Pages are plain tags: `header`, `nav`, `main`, `section`, `article`, `time`, `h1`, `h2`, `footer`. No classes. The stylesheet is a file in the content folder.

```yaml
style: paper
```

`paper` loads `content/styles/paper.css`. Shipped choices: `paper`, `ink`, `plain`, `nerd`.

A path works too. `style: style.css` uses `content/style.css`. If `style` is empty and `content/style.css` exists, that file is used.

## Templates

HTML lives in `content/templates/`. A missing file keeps the built-in page. The folder is not served.

| File | What it draws |
|---|---|
| `layout.html` | The whole page. `{{.Main}}` is the inner HTML. |
| `blog_posts.html` | Home. |
| `blog_list.html` | One post. The dot is that post. |
| `blog_post.html` | A single page. The post is `{{.Article}}`. |
| `notfound.html` | Missing pages. |

Home fields: `{{.Blurb}}`, `{{.Posts}}` (every published post, newest first), `{{.Latest}}`, `{{.HasLatest}}`, `{{.More}}` (everything after the latest). A post has `{{.Title}}`, `{{.Date}}`, `{{.DateISO}}`, `{{.URL}}`, `{{.HTML}}`, `{{.ShowTitle}}`, `{{.ShowDate}}`.

`{{range .PagesLinks}}` is every published page, by URL. A page has `{{.Title}}` and `{{.URL}}`. Drafts, posts, and `index.md` are not in it. An empty title uses the path. The layout puts these links in the nav, ahead of Feed. Change that markup to style them.

`content/templates/blog_posts.html` shows the latest post in full, then `{{.More}}` as a title and date. Edit that file to change it. The file body is the template. Leave off a `{{define}}` wrapper.

`{{.BackButton}}` prints a back link. `{{.Back}}` is just the URL. Both are empty on the home page. A post links to `/`. A nested page links to its parent when that page exists.
