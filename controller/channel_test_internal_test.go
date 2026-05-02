package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestNormalizeChannelTestEndpointUsesResponsesPolicyForAutoDetection(t *testing.T) {
	original := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   false,
		ChannelIDs:    []int{2},
		ModelPatterns: []string{`^gpt-5\.(4|5)(-.+)?$`},
	}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = original
	})

	endpoint := normalizeChannelTestEndpoint(&model.Channel{
		Id:   2,
		Type: constant.ChannelTypeOpenAI,
	}, "gpt-5.5-high", "")

	require.Equal(t, string(constant.EndpointTypeOpenAIResponse), endpoint)
}

func TestNormalizeChannelTestEndpointUsesResponseOnlyModelList(t *testing.T) {
	original := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = original
	})

	endpoint := normalizeChannelTestEndpoint(&model.Channel{
		Id:   2,
		Type: constant.ChannelTypeOpenAI,
	}, "o3-pro", "")

	require.Equal(t, string(constant.EndpointTypeOpenAIResponse), endpoint)
}

func TestNormalizeChannelTestEndpointDoesNotOverrideExplicitEndpoint(t *testing.T) {
	original := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{`^gpt-5\.`},
	}
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = original
	})

	endpoint := normalizeChannelTestEndpoint(&model.Channel{
		Id:   2,
		Type: constant.ChannelTypeOpenAI,
	}, "gpt-5.5", string(constant.EndpointTypeOpenAI))

	require.Equal(t, string(constant.EndpointTypeOpenAI), endpoint)
}

func TestChannelAutoTestExclusionDefaultsToIncluded(t *testing.T) {
	channel := &model.Channel{}

	require.False(t, channel.IsAutoTestExcluded())
}

func TestChannelAutoTestExclusionCanBeEnabled(t *testing.T) {
	excluded := true
	channel := &model.Channel{ExcludeAutoTest: &excluded}

	require.True(t, channel.IsAutoTestExcluded())
}

func TestAllChannelTestFilterOnlySkipsExcludedForAutomaticRun(t *testing.T) {
	excluded := true
	channel := &model.Channel{
		Status:          common.ChannelStatusEnabled,
		ExcludeAutoTest: &excluded,
	}

	require.False(t, shouldTestChannelInAllChannels(channel, true))
	require.True(t, shouldTestChannelInAllChannels(channel, false))
}

func TestAllChannelTestFilterAlwaysSkipsManuallyDisabled(t *testing.T) {
	channel := &model.Channel{Status: common.ChannelStatusManuallyDisabled}

	require.False(t, shouldTestChannelInAllChannels(channel, false))
}
