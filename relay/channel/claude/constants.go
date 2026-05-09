package claude

var ModelList = []string{
	"claude-3-sonnet-20240229",
	"claude-3-opus-20240229",
	"claude-3-haiku-20240307",
	"claude-3-5-haiku-20241022",
	"claude-haiku-4-5-20251001",
	"claude-3-5-sonnet-20240620",
	"claude-3-5-sonnet-20241022",
	"claude-3-7-sonnet-20250219",
	"claude-3-7-sonnet-20250219-thinking",
	"claude-sonnet-4-20250514",
	"claude-sonnet-4-20250514-thinking",
	"claude-opus-4-20250514",
	"claude-opus-4-20250514-thinking",
	"claude-opus-4-1-20250805",
	"claude-opus-4-1-20250805-thinking",
	"claude-sonnet-4-5-20250929",
	"claude-sonnet-4-5-20250929-thinking",
	"claude-opus-4-5-20251101",
	"claude-opus-4-5-20251101-thinking",
	"claude-opus-4-6",
	"claude-opus-4-6-max",
	"claude-opus-4-6-high",
	"claude-opus-4-6-medium",
	"claude-opus-4-6-low",
	"claude-sonnet-4-6",
	"claude-opus-4-7",
	"claude-opus-4-7-max",
	"claude-opus-4-7-xhigh",
	"claude-opus-4-7-high",
	"claude-opus-4-7-medium",
	"claude-opus-4-7-low",
	"claude-opus-4-7-thinking",
}

var ChannelName = "claude"

// claudeCliTestUserAgent 用于渠道测试时伪装成 Claude Code CLI,
// 以兼容部分要求 claude-cli/ 前缀的 anthropic 中转服务(如 yescode)。
// 仅用于测试流程,不影响真实 /v1/messages 请求转发。
const claudeCliTestUserAgent = "claude-cli/1.0.79 (external, cli)"
