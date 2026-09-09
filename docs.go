package main

import (
	_ "embed"
	"strings"
)

/*
What the site tells an agent that arrives with nothing but the URL. Someone says
"make a code walkthrough of <pull request> on <this site>", and everything the
agent needs has to be reachable from here: what to write, what the fields are,
and how to publish it.

/skill.md is that document, assembled from the pieces that are also the local
skill's own instructions, so the rules cannot say one thing on a laptop and
another thing here. /llms.txt is the short pointer to it, and /format is for
someone writing a second reader rather than a walkthrough.
*/

//go:embed docs/agent-intro.md
var agentIntroMD string

//go:embed docs/agent-api.md
var agentAPIMD string

//go:embed skills/code-walkthrough/RULES.md
var rulesMD string

//go:embed skills/code-walkthrough/DATA.md
var dataMD string

//go:embed spec/FORMAT.md
var formatMD string

//go:embed spec/FORMAT-v1.md
var formatV1MD string

// SkillDoc is the whole instruction set as one page. Four documents rather than
// one file, because three of them are also read somewhere else and a copy would
// drift.
func SkillDoc(base string) string {
	parts := []string{
		fill(agentIntroMD, base),
		rulesMD,
		dataMD,
		fill(agentAPIMD, base),
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func FormatDoc() string { return formatMD }

// FormatV1Doc is the older specification. Documents written against it are still
// published and still read, so the promise it makes is still a promise.
func FormatV1Doc() string { return formatV1MD }

// LLMsTxt is the front door for an agent: short enough to read in full before
// deciding, and it points at exactly one next page.
func LLMsTxt(base string) string {
	return fill(`# code walkthroughs

This site hosts code walkthroughs: a pull request or a subsystem turned into a page someone
works through at their own pace, with the code, a diagram and a diff next to the prose.

## Publishing one

Read __BASE__/skill.md first. It is the whole job in one page: how to write a walkthrough,
every field of the format, and the API to publish it. Do not work from this file alone.

Publishing needs no key. What comes back is a key for that one walkthrough, and it is the
only thing that can change it afterwards, sent as: Authorization: Bearer cwp_...
Reading needs nothing, unless the walkthrough was published with a password or limited to
a set of addresses, which the person asking for it can ask you to do.

    GET  __BASE__/skill.md                     what to write and how to publish it
    GET  __BASE__/schema.json                  the JSON schema, for validating while you write
    GET  __BASE__/format                       the format specification, for writing another reader
    GET  __BASE__/schema/v1.json               the older version, for reading what is already out there
    GET  __BASE__/format/v1                    and its specification
    POST __BASE__/api/v1/validate              check a document without storing it
    POST __BASE__/api/v1/walkthroughs          publish, and get the URL back
    PUT  __BASE__/api/v1/walkthroughs/<slug>   update in place, same URL

## When to use this

Someone asks for a code walkthrough, wants a pull request explained for their team, or wants
onboarding material for a subsystem. Read the code before you write about it: a walkthrough
assembled from a diff summary is worse than no walkthrough.

## What this is not

Not a place to host files, a site or an app. It stores one kind of document.
`, base)
}

func fill(doc, base string) string {
	return strings.ReplaceAll(doc, "__BASE__", strings.TrimRight(base, "/"))
}
