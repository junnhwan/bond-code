// Package hook 提供围绕 agent loop 的生命周期钩子注册表。
//
// 钩子是可注册的扩展点，让外部逻辑在不改 loop 主干的前提下介入工具执行与会话
// 生命周期。safety.Policy 仍是硬编码的安全不变量（blocked 不可被 --yes 绕过），
// 钩子在 safety 决策通过之后、工具执行之前/之后运行，是"额外"扩展而非 safety 的替代。
package hook

import "context"

// EventType 枚举钩子挂载的生命周期位置。
type EventType string

const (
	PreToolUse       EventType = "pre_tool_use"        // 工具执行前（safety 已批准后）
	PostToolUse      EventType = "post_tool_use"       // 工具执行后
	UserPromptSubmit EventType = "user_prompt_submit"  // 用户输入进入 loop 前
	Stop             EventType = "stop"                // loop 结束
)

// PreToolUse 钩子输入。
type PreToolUseInput struct {
	ToolName string
	Input    string // 原始 JSON 参数
	Risk     string // 工具声明的风险等级
}

// Action 取值。
const (
	ActionAllow  = "allow"  // 默认，放行
	ActionBlock  = "block"  // 阻止执行（返回 blocked 结果给模型）
	ActionModify = "modify" // 改写 input 后继续
)

// PreToolUseDecision 是 PreToolUse 钩子的返回。
type PreToolUseDecision struct {
	Action        string
	ModifiedInput string // Action=modify 时作为新 input
	Reason        string
}

// IsBlocking 报告该决策是否阻止工具执行。
func (d PreToolUseDecision) IsBlocking() bool { return d.Action == ActionBlock }

// PostToolUse 钩子输入。
type PostToolUseInput struct {
	ToolName string
	Input    string
	Output   string
	OK       bool
	Error    string
}

// PostToolUseDecision 可改写工具输出。
type PostToolUseDecision struct {
	ModifiedOutput string // 非空则替换 output
	Reason         string
}

// UserPromptSubmit 钩子。
type UserPromptSubmitInput struct {
	Prompt string
}

type UserPromptSubmitDecision struct {
	ModifiedPrompt string // 非空则替换 prompt
	Reason         string
}

// 函数钩子类型。
type (
	PreToolUseFunc       func(context.Context, PreToolUseInput) PreToolUseDecision
	PostToolUseFunc      func(context.Context, PostToolUseInput) PostToolUseDecision
	UserPromptSubmitFunc func(context.Context, UserPromptSubmitInput) UserPromptSubmitDecision
	StopFunc             func(context.Context)
)

// Registry 持有按事件分组的钩子序列。零值可用；nil 指针也安全（所有 Run 退化为 no-op）。
type Registry struct {
	preToolUse       []PreToolUseFunc
	postToolUse      []PostToolUseFunc
	userPromptSubmit []UserPromptSubmitFunc
	stop             []StopFunc
}

func (r *Registry) RegisterPreToolUse(f PreToolUseFunc) {
	if r == nil || f == nil {
		return
	}
	r.preToolUse = append(r.preToolUse, f)
}

func (r *Registry) RegisterPostToolUse(f PostToolUseFunc) {
	if r == nil || f == nil {
		return
	}
	r.postToolUse = append(r.postToolUse, f)
}

func (r *Registry) RegisterUserPromptSubmit(f UserPromptSubmitFunc) {
	if r == nil || f == nil {
		return
	}
	r.userPromptSubmit = append(r.userPromptSubmit, f)
}

func (r *Registry) RegisterStop(f StopFunc) {
	if r == nil || f == nil {
		return
	}
	r.stop = append(r.stop, f)
}

// RunPreToolUse 顺序执行钩子。任一 block 立即返回；modify 改写后续输入。
// 返回最终决策与（可能被改写的）input。
func (r *Registry) RunPreToolUse(ctx context.Context, in PreToolUseInput) (PreToolUseDecision, string) {
	if r == nil {
		return PreToolUseDecision{Action: ActionAllow}, in.Input
	}
	current := in.Input
	for _, f := range r.preToolUse {
		in.Input = current
		d := f(ctx, in)
		switch d.Action {
		case ActionBlock:
			return d, current
		case ActionModify:
			if d.ModifiedInput != "" {
				current = d.ModifiedInput
			}
		}
	}
	return PreToolUseDecision{Action: ActionAllow}, current
}

// RunPostToolUse 顺序执行钩子，output 可被逐个改写。
func (r *Registry) RunPostToolUse(ctx context.Context, in PostToolUseInput) PostToolUseInput {
	if r == nil {
		return in
	}
	for _, f := range r.postToolUse {
		d := f(ctx, in)
		if d.ModifiedOutput != "" {
			in.Output = d.ModifiedOutput
		}
	}
	return in
}

// RunUserPromptSubmit 顺序执行钩子，prompt 可被逐个改写。
func (r *Registry) RunUserPromptSubmit(ctx context.Context, in UserPromptSubmitInput) UserPromptSubmitInput {
	if r == nil {
		return in
	}
	for _, f := range r.userPromptSubmit {
		d := f(ctx, in)
		if d.ModifiedPrompt != "" {
			in.Prompt = d.ModifiedPrompt
		}
	}
	return in
}

// RunStop 执行所有 stop 钩子（loop 结束时）。
func (r *Registry) RunStop(ctx context.Context) {
	if r == nil {
		return
	}
	for _, f := range r.stop {
		f(ctx)
	}
}
