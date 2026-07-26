package command

// DirectKeyDescriptor is the canonical, dependency-neutral description of one
// user-visible direct-key interaction. ExecutionTarget identifies the semantic
// TUI route without importing Bubble Tea or the TUI package.
type DirectKeyDescriptor struct {
	ID              string
	DisplayShortcut string
	Description     string
	ExecutionTarget ExecutionTargetID
}

var builtinDirectKeyDescriptors = []DirectKeyDescriptor{
	{ID: "key.submit", DisplayShortcut: "Enter", Description: "submit prompt", ExecutionTarget: "tui-local.submit"},
	{ID: "key.newline", DisplayShortcut: "Ctrl+J / Alt+Enter / Shift+Enter", Description: "insert newline (Ctrl+J is the reliable Windows path)", ExecutionTarget: "tui-local.composer.newline"},
	{ID: "key.escape", DisplayShortcut: "Esc", Description: "cancel run or dismiss active overlay", ExecutionTarget: "tui-local.cancel"},
	{ID: "key.interrupt", DisplayShortcut: "Ctrl+C", Description: "interrupt; repeated press exits", ExecutionTarget: "tui-local.interrupt"},
	{ID: "key.exit-empty", DisplayShortcut: "Ctrl+D", Description: "exit when input is empty", ExecutionTarget: "tui-local.exit.empty"},
	{ID: "key.mode-cycle", DisplayShortcut: "Shift+Tab / Alt+M", Description: "cycle plan/normal mode (Alt+M is the Windows fallback)", ExecutionTarget: "tui-local.mode.cycle"},
	{ID: "key.details", DisplayShortcut: "Ctrl+O", Description: "toggle expanded tool details", ExecutionTarget: "tui-local.view.verbose"},
	{ID: "key.thinking", DisplayShortcut: "Ctrl+T", Description: "toggle historical thinking blocks", ExecutionTarget: "tui-local.view.thinking"},
	{ID: "key.history-search", DisplayShortcut: "Ctrl+R", Description: "search prompt history in reverse", ExecutionTarget: "tui-local.history.reverse"},
	{ID: "key.agent-switcher", DisplayShortcut: "Ctrl+Up", Description: "open Agent switcher", ExecutionTarget: "tui-local.agent.switcher"},
	{ID: "key.external-editor", DisplayShortcut: "Ctrl+G", Description: "open draft in external editor", ExecutionTarget: "tui-local.prompt.editor"},
	{ID: "key.stash", DisplayShortcut: "Ctrl+S", Description: "stash or restore draft", ExecutionTarget: "tui-local.prompt.stash"},
	{ID: "key.redraw", DisplayShortcut: "Ctrl+L", Description: "redraw screen without clearing session state", ExecutionTarget: "tui-local.screen.redraw"},
}

// DirectKeyDescriptors returns the canonical direct-key policy in stable help
// order. The returned slice is a defensive copy.
func DirectKeyDescriptors() []DirectKeyDescriptor {
	return append([]DirectKeyDescriptor(nil), builtinDirectKeyDescriptors...)
}

// LookupDirectKeyDescriptor resolves a canonical stable direct-key ID.
func LookupDirectKeyDescriptor(id string) (DirectKeyDescriptor, bool) {
	for _, descriptor := range builtinDirectKeyDescriptors {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return DirectKeyDescriptor{}, false
}
