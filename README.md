# tinypaste

A tiny, no-nonsense pastebin. No registration, no tracking, just paste and share.

This is a super simple, file-based paste service that I built because I love tiny, useful things that do one job well. It's fast, CLI-friendly, and pastes automatically expire. The whole project follows a "secure by design" philosophy with some neat optimizations under the hood. If you're curious about the technical details and why I didn't use a database, check out the github wiki page for full project story and more technical details.

## Using It

You can use tinypaste from the browser or your terminal.

From your browser, just visit the site, paste your stuff, and grab the link.

From your terminal, use this command to post content and get the paste link:

```bash
curl -sL -w "%{url_effective}\n" -o /dev/null -X POST -d "title=my test&body=hello from the terminal" http://localhost:8080/save
```

## Deploy Your Own

With Dokku (Recommended):

```bash
# Create the app and mount a volume for the pastes
dokku apps:create tinypaste
dokku storage:ensure-directory tinypaste
dokku storage:mount tinypaste /var/lib/dokku/data/storage/tinypaste:/app/pastes

# Deploy it
git push dokku main
```

Manually:

```bash
# Just build and run it directly with Go
go build && ./tinypaste
```

By default, it runs on port `8080`. Set the `PORT` environment variable to change it.

## Rate Limiting

Built-in nginx rate limiting prevents abuse:
- `/save`: 2 requests/minute (paste creation)
- `/[id]`: 30 requests/minute (viewing pastes)  
- `/`: 60 requests/minute (general browsing)