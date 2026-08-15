package corpus

import (
	"fmt"
	"strings"
)

// Role and GroundingRules are the behavioural halves of the system prompt,
// split so both grounding modes can share them: the documents are either
// inlined below the instructions (this package) or retrieved on demand
// (package rag). Only the sentence describing where the documents live differs.
//
// GroundingRules carries the one rule that decides whether this assistant is
// useful or dangerous — never invent experience. The framing mirrors the guard
// rails already in this codebase: lumi-neo blocks an assistant that claims
// "saved!" with no write behind it; the same principle blocks one that claims
// experience with no résumé line behind it. A fabricated bullet point is worse
// than a missing one, because the candidate has to defend it in an interview.
const Role = `You are a career assistant. You help the candidate reason about their own experience in the context of a specific role: answering questions about their background, comparing it to what the role asks for, and drafting material they can actually use — talking points, bullet rewrites, cover-letter paragraphs, interview answers.`

const GroundingRules = `Ground every claim about the candidate in the résumé:
- State experience only where the résumé supports it. Name the role, project, or section you are drawing from, so the candidate can check you.
- Never invent employers, titles, dates, technologies, or metrics. If the résumé does not say it, say that it does not — then offer what the résumé does support.
- When drafting anything the candidate would send or say, build it only from résumé content. Rephrasing and reframing are fine; adding facts is not.

Be straight about fit:
- Name real matches AND real gaps. A gap stated plainly is more useful than one papered over, because the candidate can decide how to address it.
- Where the résumé shows adjacent-but-not-identical experience, say exactly that, and how close it actually is.
- If the job description is ambiguous about a requirement, say so rather than guessing what the employer meant.

If a question needs information in neither document — salary history, notice period, something from another project — say what you would need and ask for it.

Keep answers as short as the question deserves. The candidate is looking at their own résumé; they do not need it recited back.`

// SystemPrompt composes the instructions with the full text of every document.
// Used when retrieval is unavailable — the corpus is small enough that inlining
// is a legitimate fallback rather than a degraded one. The string is stable
// across turns, which is what makes it worth caching (see model.AnthropicClient,
// where the system block carries a cache breakpoint).
func (c *Corpus) SystemPrompt() string {
	if c.Empty() {
		return ""
	}

	var b strings.Builder
	b.WriteString(Role)
	b.WriteString("\n\nThe candidate's résumé and the job description under discussion are included in full below.\n\n")
	b.WriteString(GroundingRules)

	writeSection := func(heading string, docs []Document, note string) {
		if len(docs) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n\n# %s\n", heading)
		if note != "" {
			fmt.Fprintf(&b, "%s\n", note)
		}
		for _, d := range docs {
			fmt.Fprintf(&b, "\n## Source: %s\n\n%s\n", d.Name, d.Text)
		}
	}

	writeSection("Résumé", c.Of(KindResume),
		"This is the authority on the candidate's experience. Anything not here is not established.")
	writeSection("Job description", c.Of(KindJobDescription),
		"The role under discussion. Where it arrived as an email, the surrounding correspondence is included as sent.")
	writeSection("Supporting documents", c.Of(KindSupporting), "")

	return b.String()
}
