# Marketplace

The marketplace is a catalog of open-source desktop applications that anyone can extend. The
launcher reads it from the website and shows it in the Marketplace tab.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/market/catalog?lang=de` | list of entries, localised descriptions |
| `GET` | `/api/market/app?id=…&lang=de` | one entry with details and reviews |
| `POST` | `/api/market/submit` | submit or edit your own entry (bearer token) |
| `POST` | `/api/market/review` | review somebody else's entry |
| `POST` | `/api/market/report` | report a problem |
| `POST` | `/api/market/suggest` | suggest an edit |

`lang` accepts the 15 UI languages; without it the server uses `Accept-Language`, then English.
Descriptions come back localized when a translation exists — `descriptionLang` and
`descriptionTranslated` tell you which one you got.

## Entry fields

`id`, `repo` (`owner/name`), `name`, `description`, `category`, `tags[]`, `stars`, `license`,
`platforms[]`, `releaseTag`, `verified`, `officialMark`, `supportCount`, `rating`, `reviews`,
`author{handle, name, verifiedDeveloper}`.

## Rules

- only `github.com` repositories
- no forks without changes, no abandoned projects, no closed-source binaries
- the entry must install and start with the launcher on at least one platform
- licence must allow redistribution of the binaries
- support counters are public numbers only; the list of supporters is never shown
- reviews and ratings come from real accounts, nothing is generated

## Moderation

Submissions land in the admin queue (the admin area is owner-only). Actions: approve, request
changes, reject with a reason, mark as official. Every action is appended to the moderation log.
