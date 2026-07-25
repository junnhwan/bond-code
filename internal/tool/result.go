package tool

type Result struct {
	ToolName string         `json:"tool_name"`
	Status   string         `json:"status"`
	Summary  string         `json:"summary,omitempty"`
	Output   string         `json:"output"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	OK       bool           `json:"ok"`
}

const (
	StatusSuccess  = "success"
	StatusError    = "error"
	StatusBlocked  = "blocked"
	StatusRejected = "rejected"
	StatusGuarded  = "guarded"
)

func Success(name, summary, output string) *Result {
	return &Result{
		ToolName: name,
		Status:   StatusSuccess,
		Summary:  summary,
		Output:   output,
		OK:       true,
	}
}

func ErrorResult(name, summary, err string) *Result {
	return &Result{
		ToolName: name,
		Status:   StatusError,
		Summary:  summary,
		Output:   "error: " + err,
		Error:    err,
		OK:       false,
	}
}

func Guarded(name, summary, output string) *Result {
	return &Result{
		ToolName: name,
		Status:   StatusGuarded,
		Summary:  summary,
		Output:   output,
		Error:    output,
		OK:       false,
	}
}

func Blocked(name, summary, err string) *Result {
	result := ErrorResult(name, summary, err)
	result.Status = StatusBlocked
	return result
}

func Rejected(name, summary, err string) *Result {
	result := ErrorResult(name, summary, err)
	result.Status = StatusRejected
	return result
}

func NormalizeResult(result *Result, toolName string) *Result {
	if result == nil {
		return ErrorResult(toolName, "tool returned no result", "tool returned nil result")
	}
	if result.ToolName == "" {
		result.ToolName = toolName
	}
	if result.Status == "" {
		if result.OK {
			result.Status = StatusSuccess
		} else {
			result.Status = StatusError
		}
	}
	if result.Error != "" && result.Output == "" {
		result.Output = "error: " + result.Error
	}
	return result
}
