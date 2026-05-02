package codex

var ModelList = []string{
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"gpt-5.6-luna",
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.3-codex-spark",
	"codex-auto-review",
	CodexImageModel,
}

const (
	CodexImageModel        = "gpt-image-2"
	defaultImagesMainModel = "gpt-5.4-mini"
	imageGenerationTool    = "image_generation"
)
const ChannelName = "codex"
