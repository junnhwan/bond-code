package contextx

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/junnhwan/bond-code/internal/llm"
)

// FileOperations tracks paths touched in a message span (Pi FileOperations).
type FileOperations struct {
	Read    map[string]bool
	Edited  map[string]bool
	Written map[string]bool
}

func newFileOps() FileOperations {
	return FileOperations{
		Read:    map[string]bool{},
		Edited:  map[string]bool{},
		Written: map[string]bool{},
	}
}

func extractFileOperations(messages []Message) FileOperations {
	ops := newFileOps()
	for _, msg := range messages {
		extractFileOpsFromMessage(msg, &ops)
	}
	return ops
}

func mergeFileOps(dst *FileOperations, src FileOperations) {
	for p := range src.Read {
		dst.Read[p] = true
	}
	for p := range src.Edited {
		dst.Edited[p] = true
	}
	for p := range src.Written {
		dst.Written[p] = true
	}
}

func extractFileOpsFromMessage(msg Message, ops *FileOperations) {
	if msg.Role == llm.RoleAssistant {
		for _, tc := range msg.ToolCalls {
			path := pathFromToolArgs(tc.Arguments)
			if path == "" {
				continue
			}
			switch tc.Name {
			case "read_file", "list_dir", "search_text":
				ops.Read[path] = true
			case "write_file":
				ops.Written[path] = true
			case "edit_file":
				ops.Edited[path] = true
			}
		}
		return
	}
	if msg.Role == llm.RoleTool {
		path := extractPath(msg.Content)
		if path == "" {
			return
		}
		switch {
		case isReadTool(msg.ToolName):
			ops.Read[path] = true
		case isWriteTool(msg.ToolName):
			ops.Written[path] = true
		case msg.ToolName == "edit_file":
			ops.Edited[path] = true
		}
	}
}

func pathFromToolArgs(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return ""
	}
	return extractDirectPath(parsed)
}

type fileLists struct {
	ReadFiles     []string
	ModifiedFiles []string
}

func computeFileLists(ops FileOperations) fileLists {
	modified := map[string]bool{}
	for p := range ops.Edited {
		modified[p] = true
	}
	for p := range ops.Written {
		modified[p] = true
	}
	readOnly := make([]string, 0)
	for p := range ops.Read {
		if !modified[p] {
			readOnly = append(readOnly, p)
		}
	}
	mod := make([]string, 0, len(modified))
	for p := range modified {
		mod = append(mod, p)
	}
	sortStrings(readOnly)
	sortStrings(mod)
	return fileLists{ReadFiles: readOnly, ModifiedFiles: mod}
}

func formatFileOperations(readFiles, modifiedFiles []string) string {
	var sections []string
	if len(readFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(readFiles, "\n")+"\n</read-files>")
	}
	if len(modifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(modifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sections, "\n\n")
}

func filePathsToObservations(paths []string, reads bool) []FileObservation {
	now := time.Now().UTC()
	out := make([]FileObservation, 0, len(paths))
	tool := "write_file"
	if reads {
		tool = "read_file"
	}
	for _, p := range paths {
		out = append(out, FileObservation{Path: p, ToolName: tool, At: now})
	}
	return out
}

func sortStrings(values []string) {
	// simple insertion sort — lists are small
	for i := 1; i < len(values); i++ {
		j := i
		for j > 0 && values[j-1] > values[j] {
			values[j-1], values[j] = values[j], values[j-1]
			j--
		}
	}
}
