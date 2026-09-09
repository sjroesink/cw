# The API

Base URL: `__BASE__`

**Publishing needs no key.** What comes back from a publish is a key for that one walkthrough, and
it is the only thing that can change it afterwards. Hand it to the person you are working for, and
keep it out of your shell history, out of the repository, and out of anything you write down for
someone else to read. Reading needs nothing at all.

## Validate before you publish

```sh
curl -sS -X POST __BASE__/api/v1/validate \
  -H "Content-Type: application/json" \
  --data-binary @walkthrough.json
```

```json
{ "ok": false,
  "errors": ["parts[0].sections[1].steps[0].code.hi[0] points at line 44, and the snippet runs 18 to 31"],
  "warnings": ["parts[2] has no desc, so its card is a title on its own"] }
```

Errors mean the document is wrong and it will not be stored. Warnings do not block anything and are
usually still worth fixing: they are the difference between a walkthrough that validates and one
someone wants to read.

## Publish

```sh
curl -sS -X POST __BASE__/api/v1/walkthroughs \
  -H "Content-Type: application/json" \
  --data-binary @walkthrough.json
```

```json
{ "ok": true,
  "slug": "fincent-pr-3347",
  "url": "__BASE__/w/fincent-pr-3347",
  "key": "cwp_9c1f...",
  "warnings": [] }
```

`key` is shown once and never again. Only its hash is stored, so a lost key means asking whoever
runs the site.

The name is derived from `source`, or from the title when there is no source. To choose it yourself,
send `{"slug": "...", "walkthrough": { ... }}` instead of the bare document. A name that is taken
gets a number appended, and asking for a taken name outright is refused rather than overwriting what
is already there.

## Locking it

The same envelope decides who may read it. Both are optional and independent, and a walkthrough that
sets both needs both: being at an allowed address is not the same claim as knowing the password.

```json
{ "slug": "fincent-pr-3347",
  "password": "the one you were told to use",
  "allow": ["203.0.113.0/24", "198.51.100.7"],
  "walkthrough": { }}
```

A locked walkthrough is not listed to anyone who has not opened it, so its title does not leak from
the front page. Never invent a password: use the one the person asked for, and hand it back to them
in your reply rather than leaving it only in a command.

On an update, a field that is not sent leaves that half of the lock exactly as it was, so changing
the content of a protected walkthrough does not quietly unprotect it. An empty value takes it off:
`"password": ""` removes the password, `"allow": []` removes the address list.

`GET /api/v1/whoami` says which address the site thinks you are calling from. Use it before writing
an address list, so the list does not lock out the person who wrote it.

Missing `id`s and missing snippet `sha`/`to` values are filled in on publish and come back in the
stored document, so you do not have to write them by hand.

## Update, in place

```sh
curl -sS -X PUT __BASE__/api/v1/walkthroughs/<slug> \
  -H "Authorization: Bearer $CW_KEY" \
  -H "Content-Type: application/json" \
  --data-binary @walkthrough.json
```

Same URL, new content, and no second key: the one from the first publish keeps working. Anyone with
the page open is offered a reload.

Use this rather than publishing again. A second `POST` makes a second walkthrough at a different
URL, and the link you already handed out keeps showing the old one.

## The rest

| | |
|---|---|
| `GET /api/v1/walkthroughs` | everything published, newest first |
| `GET /api/v1/walkthroughs/<slug>` | the document and its metadata back |
| `DELETE /api/v1/walkthroughs/<slug>` | remove it. Needs that walkthrough's key |
| `POST /api/v1/walkthroughs/<slug>/unlock` | `{"password": "..."}`, and the reply sets a cookie for that one walkthrough |
| `GET /api/v1/whoami` | the address you appear to be calling from |
| `GET /schema.json` | the schema, for validating while you write |
| `GET /schema/v1.json` | the older version of it, for reading what is already published |
| `GET /format` | the format specification, for writing a second reader |

## Failures

| | |
|---|---|
| `400` | the document is wrong. `errors` says where, in the same words the local tool uses |
| `401` | changing something without its key, or reading something locked without its password |
| `403` | reading something that is limited to addresses yours is not among |
| `404` | no walkthrough by that name |
| `409` | the name you asked for is taken by someone else's walkthrough |
| `413` | over the size limit. A walkthrough is prose and snippets, not an archive |
