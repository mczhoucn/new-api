package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelSensitiveTestContext(t *testing.T, settings dto.ChannelSettings) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelSetting, settings)
	return ctx
}

func channelSensitiveTestRequest() *dto.GeneralOpenAIRequest {
	return &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "this prompt contains blocked content"}},
	}
}

func TestCheckChannelSensitiveWordsDisabled(t *testing.T) {
	ctx := newChannelSensitiveTestContext(t, dto.ChannelSettings{SensitiveWords: []string{"blocked"}})
	request := channelSensitiveTestRequest()
	meta := request.GetTokenCountMeta()

	require.Nil(t, CheckChannelSensitiveWords(ctx, request, nil, &meta))
}

func TestCheckChannelSensitiveWordsRejectsBeforeRetry(t *testing.T) {
	ctx := newChannelSensitiveTestContext(t, dto.ChannelSettings{
		SensitiveCheckEnabled: true,
		SensitiveWords:        []string{"blocked"},
	})
	request := channelSensitiveTestRequest()
	meta := &relaytypes.TokenCountMeta{TokenType: relaytypes.TokenTypeTokenizer}

	apiErr := CheckChannelSensitiveWords(ctx, request, nil, &meta)

	require.NotNil(t, apiErr)
	assert.Equal(t, relaytypes.ErrorCodeSensitiveWordsDetected, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.True(t, relaytypes.IsSkipRetryError(apiErr))
	assert.Contains(t, meta.CombineText, "blocked")
}

func TestCheckChannelSensitiveWordsUsesChannelFallback(t *testing.T) {
	ctx := newChannelSensitiveTestContext(t, dto.ChannelSettings{})
	ctx.Keys = nil
	setting := `{"sensitive_check_enabled":true,"sensitive_words":["blocked"]}`
	channel := &model.Channel{Setting: &setting}

	apiErr := CheckChannelSensitiveWords(ctx, channelSensitiveTestRequest(), channel, nil)

	require.NotNil(t, apiErr)
	assert.Equal(t, relaytypes.ErrorCodeSensitiveWordsDetected, apiErr.GetErrorCode())
}

func TestCheckChannelSensitiveWordsPrefersCurrentContext(t *testing.T) {
	ctx := newChannelSensitiveTestContext(t, dto.ChannelSettings{
		SensitiveCheckEnabled: true,
		SensitiveWords:        []string{"blocked"},
	})
	setting := `{"sensitive_check_enabled":true,"sensitive_words":["channel-only"]}`
	channel := &model.Channel{Setting: &setting}
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "channel-only"}},
	}
	meta := request.GetTokenCountMeta()

	assert.Nil(t, CheckChannelSensitiveWords(ctx, request, channel, &meta))
}

func TestCheckChannelSensitiveWordsIgnoresBlankWords(t *testing.T) {
	ctx := newChannelSensitiveTestContext(t, dto.ChannelSettings{
		SensitiveCheckEnabled: true,
		SensitiveWords:        []string{" ", "\t"},
	})

	assert.Nil(t, CheckChannelSensitiveWords(ctx, channelSensitiveTestRequest(), nil, nil))
}
