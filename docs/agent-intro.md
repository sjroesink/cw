# Publishing a code walkthrough here

You are reading this because someone asked for a code walkthrough on this site. Everything you need
is on this page: the rules for writing one, the field reference for the file, and the API to publish
it. You do not need any local tool.

## What a walkthrough is

A page someone works through at their own pace. An overview of what the change is made of, and
inside each part the sections and steps that explain it, with the code, a diagram, a diff or a small
animation next to the prose. Progress is remembered per reader, and every snippet links back to the
line it came from.

It is one JSON document. The page holds no topic of its own, so writing a walkthrough is writing
that file and nothing else.

## What you do

1. **Read the change.** For a pull request: `gh pr view <n>`, `gh pr diff <n>`, and
   `gh pr view <n> --json files,headRefOid,baseRefName`. Read the files themselves, not only the
   diff. You cannot write about code you have not opened.
2. **Write the JSON**, against the rules and the field reference below. Fill in `source` with the
   repository, the pull request URL, the head commit and the changed files: that is what turns every
   snippet into a link back to the diff.
3. **Validate it** by posting it to `/api/v1/validate`. Fix what comes back and post again.
4. **Publish it** with `POST /api/v1/walkthroughs`. No key is needed to do that. Give the person
   the URL that comes back, and the key that comes with it: it is shown once, and it is the only
   thing that can change that walkthrough afterwards.

## What to tell them afterwards

The URL, one line per part on what it covers, and which files the walkthrough touches. Say what you
could not verify, and name any snippet you were not able to read in full.
