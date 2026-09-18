package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relaydto "github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newTaskSensitiveTestContext(t *testing.T, request any) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelSetting, relaydto.ChannelSettings{
		SensitiveCheckEnabled: true,
		SensitiveWords:        []string{"blocked"},
	})
	ctx.Set("task_request", request)
	return ctx
}

func TestCheckTaskSensitiveWordsDetectsGenericPrompt(t *testing.T) {
	ctx := newTaskSensitiveTestContext(t, relaycommon.TaskSubmitReq{Prompt: "this prompt contains blocked content"})

	taskErr := CheckTaskSensitiveWords(ctx, nil)

	require.NotNil(t, taskErr)
	require.Equal(t, string(relaytypes.ErrorCodeSensitiveWordsDetected), taskErr.Code)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.True(t, taskErr.LocalError)
	require.True(t, taskErr.NoRetry)
}

func TestCheckTaskSensitiveWordsDetectsSunoPrompts(t *testing.T) {
	for _, request := range []any{
		&appdto.SunoSubmitReq{Prompt: "blocked"},
		&appdto.SunoSubmitReq{GptDescriptionPrompt: "blocked"},
		map[string]any{"prompt": "blocked", "title": "a song"},
	} {
		ctx := newTaskSensitiveTestContext(t, request)
		taskErr := CheckTaskSensitiveWords(ctx, nil)
		require.NotNil(t, taskErr)
		require.Equal(t, string(relaytypes.ErrorCodeSensitiveWordsDetected), taskErr.Code)
	}
}

func TestCheckTaskSensitiveWordsAllowsCleanPrompt(t *testing.T) {
	ctx := newTaskSensitiveTestContext(t, relaycommon.TaskSubmitReq{Prompt: "clean prompt"})

	require.Nil(t, CheckTaskSensitiveWords(ctx, nil))
}

func TestCheckTaskSensitiveWordsFallsBackToChannel(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	setting := `{"sensitive_check_enabled":true,"sensitive_words":["blocked"]}`
	channel := &model.Channel{Setting: &setting}
	ctx.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "blocked"})

	taskErr := CheckTaskSensitiveWords(ctx, channel)

	require.NotNil(t, taskErr)
	require.Equal(t, string(relaytypes.ErrorCodeSensitiveWordsDetected), taskErr.Code)
}
