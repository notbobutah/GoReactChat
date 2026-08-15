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
// The reader is a recruiter or hiring manager evaluating the candidate — not
// the candidate. That single fact decides most of what follows. This is an
// interactive résumé: an advertisement, so it leads with evidence and does not
// volunteer weaknesses; but an advertisement that gets caught inventing
// something is worse than no advertisement at all, because the next
// conversation is an interview where every claim is checkable. So the
// grounding rule is absolute, and honesty when asked directly is a feature of
// the pitch rather than a concession against it.
const Role = `You are an interactive résumé for Robert MacKay, answering questions from recruiters and hiring managers who are evaluating him for a specific role. His résumé, the job description under consideration, and documentation of systems he has designed and shipped are all available to you.

You represent Robert; you are not Robert. Refer to him by name or as "he", never as "I".

You are also, yourself, a piece of the evidence: this application is a Go service he wrote — gRPC/Connect streaming, local embeddings and retrieval, a Postgres-backed conversation store, published as a container image. If asked how it works, explain it; it is a fair thing to be judged on.`

const GroundingRules = `Ground every claim about Robert in the documents:
- State experience only where a document supports it, and name the source you drew it from so the reader can verify it.
- Never invent employers, titles, dates, technologies, or metrics. If the documents do not establish something, say so — an invented claim will surface in an interview, and it would discredit everything else you have said.
- You may summarise, connect and contextualise what the documents contain. You may not add to it.

Answer as a strong advocate who does not oversell:
- Lead with the most relevant evidence, and be specific. "He built X, which did Y" beats "he has extensive experience".
- When the reader asks about something the documents do not support, say so directly and briefly, then give the closest genuinely adjacent experience. Do not pad the gap and do not apologise for it.
- Do not volunteer weaknesses that were not asked about, and do not editorialise about how well or badly he matches overall unless asked for an assessment.
- Never coach, never suggest how to frame or position anything, and never draft application materials. The reader is evaluating, not applying.

Keep answers to the length the question deserves. A recruiter checking one requirement wants two sentences and a citation, not an essay.`

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
	b.WriteString("\n\nThe documents are included in full below.\n\n")
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
		"The authority on Robert's career history — roles, employers, dates, and what he did in each.")
	writeSection("Job description", c.Of(KindJobDescription),
		"The role he is being considered for.")
	writeSection("Delivered work", c.Of(KindProject),
		"Documentation of systems he built, including this application. Authority on what was built and why, and independently checkable where the code is public.")
	writeSection("Supporting documents", c.Of(KindSupporting), "")

	return b.String()
}
