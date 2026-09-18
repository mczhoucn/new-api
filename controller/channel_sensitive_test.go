package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLockedTaskAttemptLoadsCurrentChannelSensitiveSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	setting := `{"sensitive_check_enabled":true,"sensitive_words":["blocked"]}`
	channel := &model.Channel{
		Id:      12345,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "locked-task-channel",
		Key:     "sk-test",
		Setting: &setting,
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "sora-2",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{LockedChannel: channel},
	}

	selected, taskErr := setupLockedTaskChannelForAttempt(ctx, info)
	defer service.ReleaseChannelConcurrencyLease(ctx)

	require.Nil(t, taskErr)
	require.Same(t, channel, selected)
	channelSetting, ok := common.GetContextKeyType[dto.ChannelSettings](ctx, constant.ContextKeyChannelSetting)
	require.True(t, ok)
	require.True(t, channelSetting.SensitiveCheckEnabled)
	require.Equal(t, []string{"blocked"}, channelSetting.SensitiveWords)
	require.Equal(t, channel.Id, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))

	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "contains blocked text"}},
	}
	meta := request.GetTokenCountMeta()
	apiErr := relay.CheckChannelSensitiveWords(ctx, request, selected, &meta)
	require.Error(t, apiErr)
	require.Equal(t, types.ErrorCodeSensitiveWordsDetected, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
}
