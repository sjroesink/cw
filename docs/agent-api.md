# The API

Base URL: `__BASE__`

Publishing and updating need a key: `Authorization: Bearer cw_…`. Ask the person you are working for
if you do not have one, and keep it out of your shell history and out of anything you write down.
Reading a walkthrough, and listing what is published, need no key.

## Validate before you publish

```sh
curl -sS -X POST __BASE__/api/v1/validate \
  -H "Authorization: Bearer $CW_API_KEY" \
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
  -H "Authorization: Bearer $CW_API_KEY" \
  -H "Content-Type: application/json" \
  --data-binary @walkthrough.json
```

```json
{ "ok": true,
  "slug": "fincent-pr-3347",
  "url": "__BASE__/w/fincent-pr-3347",
  "warnings": [] }
```

The name is derived from `source`, or from the title when there is no source. To choose it yourself,
send `{"slug": "...", "walkthrough": { ... }}` instead of the bare document. A name that is taken
gets a number appended rather than overwriting what is there.

Missing `id`s and missing snippet `sha`/`to` values are filled in on publish and come back in the
stored document, so you do not have to write them by hand.

## Update, in place

```sh
curl -sS -X PUT __BASE__/api/v1/walkthroughs/<slug> \
  -H "Authorization: Bearer $CW_API_KEY" \
  -H "Content-Type: application/json" \
  --data-binary @walkthrough.json
```

Same URL, new content. Anyone with the page open is offered a reload. Use this rather than
publishing again: a second `POST` makes a second walkthrough at a different URL, and the link you
already handed out keeps showing the old one.

## The rest

| | |
|---|---|
| `GET /api/v1/walkthroughs` | everything published, newest first |
| `GET /api/v1/walkthroughs/<slug>` | the document and its metadata back |
| `DELETE /api/v1/walkthroughs/<slug>` | remove it. Needs a key |
| `GET /schema/v1.json` | the schema, for validating while you write |
| `GET /format` | the format specification, for writing a second reader |

## Failures

| | |
|---|---|
| `400` | the document is wrong. `errors` says where, in the same words the local tool uses |
| `401` | no key, or a key that is not known here. Do not retry; ask for a key |
| `404` | no walkthrough by that name |
| `409` | that name is taken by someone else's walkthrough |
| `413` | over the size limit. A walkthrough is prose and snippets, not an archive |
