package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// CheckChannelSensitiveWords checks a request against the policy of the
// channel selected for the current attempt. It deliberately does not perform
// billing or channel retry decisions: callers must invoke it after selecting
// and setting up the channel, but before reserving quota or sending upstream.
func CheckChannelSensitiveWords(
	c *gin.Context,
	request dto.Request,
	channel *model.Channel,
	meta **types.TokenCountMeta,
) *types.NewAPIError {
	if request == nil {
		return nil
	}
	if meta == nil {
		var localMeta *types.TokenCountMeta
		meta = &localMeta
	}

	channelSetting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if !ok {
		channelSetting = dto.ChannelSettings{}
		if channel != nil {
			channelSetting = channel.GetSetting()
		}
	}
	if !channelSetting.SensitiveCheckEnabled {
		return nil
	}

	// A fast pricing-only meta intentionally omits CombineText. Build the full
	// request metadata only when a selected channel actually enables filtering.
	if *meta == nil || (*meta).CombineText == "" {
		*meta = request.GetTokenCountMeta()
	}
	if *meta == nil || (*meta).CombineText == "" {
		return nil
	}

	contains, words := service.CheckSensitiveTextWithWords((*meta).CombineText, channelSetting.SensitiveWords)
	if !contains {
		return nil
	}

	message := fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", "))
	logger.LogWarn(c, message)
	return types.NewError(
		errors.New("sensitive words detected"),
		types.ErrorCodeSensitiveWordsDetected,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithStatusCode(http.StatusBadRequest),
	)
}
