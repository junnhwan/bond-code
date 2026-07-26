package skill

// Source identifies where a skill was loaded from.
type Source string

const (
	SourceUser    Source = "user"
	SourceProject Source = "project"
	SourceRoot    Source = "root" // skills.root override / extra dir
)

// Skill is a local SKILL.md workflow (Claude Code-style prompt command).
// Index listing only needs metadata; Body is filled on Load/Expand.
type Skill struct {
	Name                   string
	Description            string
	WhenToUse              string
	ArgumentHint           string
	AllowedTools           []string
	DisableModelInvocation bool
	UserInvocable          bool
	Source                 Source
	// Dir is the skill directory (parent of SKILL.md); used as base path prefix.
	Dir string
	// Path is the absolute path to SKILL.md.
	Path string
	// Body is markdown after frontmatter (only set after full load).
	Body string
	// ContentLength is the raw file size in bytes (for status/budget).
	ContentLength int
}

// ModelInvocable reports whether the model may call the Skill tool for this entry.
func (s Skill) ModelInvocable() bool {
	return !s.DisableModelInvocation
}

// SlashInvocable reports whether the user may type /name to expand this skill.
// Claude Code: user-invocable defaults true; false hides from the slash menu.
func (s Skill) SlashInvocable() bool {
	return s.UserInvocable
}

// ListingDescription is the discovery blurb (description + optional when_to_use).
func (s Skill) ListingDescription() string {
	desc := s.Description
	if s.WhenToUse != "" {
		if desc == "" {
			return s.WhenToUse
		}
		return desc + " - " + s.WhenToUse
	}
	return desc
}
