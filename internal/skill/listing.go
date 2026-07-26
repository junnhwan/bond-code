package skill

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DefaultListingBudget is ~1% of a 200k-token window in characters (CC default).
const DefaultListingBudget = 8000

// MaxListingDescChars hard-caps each skill's description in the listing (CC: 250).
const MaxListingDescChars = 250

// FormatListing renders a budgeted skill discovery list for the dynamic reminder.
func FormatListing(skills []Skill, budget int) string {
	if len(skills) == 0 {
		return ""
	}
	if budget <= 0 {
		budget = DefaultListingBudget
	}
	type entry struct {
		name string
		full string
	}
	entries := make([]entry, 0, len(skills))
	for _, s := range skills {
		if !s.ModelInvocable() {
			continue
		}
		desc := s.ListingDescription()
		if utf8.RuneCountInString(desc) > MaxListingDescChars {
			desc = truncateRunes(desc, MaxListingDescChars-1) + "…"
		}
		line := "- " + s.Name
		if desc != "" {
			line += ": " + desc
		}
		entries = append(entries, entry{name: s.Name, full: line})
	}
	if len(entries) == 0 {
		return ""
	}

	var full strings.Builder
	for i, e := range entries {
		if i > 0 {
			full.WriteByte('\n')
		}
		full.WriteString(e.full)
	}
	if full.Len() <= budget {
		return full.String()
	}

	nameOverhead := 0
	for i, e := range entries {
		nameOverhead += len("- " + e.name)
		if i > 0 {
			nameOverhead++
		}
	}
	remaining := budget - nameOverhead
	if remaining < len(entries)*20 {
		var names strings.Builder
		for i, e := range entries {
			if i > 0 {
				names.WriteByte('\n')
			}
			names.WriteString("- ")
			names.WriteString(e.name)
		}
		return names.String()
	}
	maxDesc := remaining / len(entries)
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		desc := strings.TrimPrefix(e.full, "- "+e.name)
		desc = strings.TrimPrefix(desc, ": ")
		if desc != "" && len(desc) > maxDesc {
			desc = truncateRunes(desc, maxDesc)
		}
		b.WriteString("- ")
		b.WriteString(e.name)
		if desc != "" {
			b.WriteString(": ")
			b.WriteString(desc)
		}
	}
	return b.String()
}

// FormatIndex is a human-facing listing for /skills (no budget pressure).
// Lines stay single logical entries; the TUI wraps them to the terminal width.
func FormatIndex(skills []Skill) string {
	if len(skills) == 0 {
		return "No skills found."
	}
	modelN, userOnlyN, slashN := 0, 0, 0
	for _, item := range skills {
		if item.ModelInvocable() {
			modelN++
		} else {
			userOnlyN++
		}
		if item.SlashInvocable() {
			slashN++
		}
	}
	lines := make([]string, 0, len(skills)+2)
	lines = append(lines, fmt.Sprintf(
		"%d skills · %d model-invocable · %d user-only · type /<name> for %d slash-invocable",
		len(skills), modelN, userOnlyN, slashN,
	))
	for _, item := range skills {
		line := "- " + item.Name
		if d := item.ListingDescription(); d != "" {
			line += ": " + d
		}
		if item.DisableModelInvocation {
			line += " [user-only]"
		}
		if !item.SlashInvocable() {
			line += " [model-only]"
		}
		if item.Source != "" {
			line += " (" + string(item.Source) + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
