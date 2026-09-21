package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	appdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelConcurrencyLimitErrorDoesNotDisableOrRecord(t *testing.T) {
	original := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	defer func() {
		common.AutomaticDisableChannelEnabled = original
	}()

	err := types.NewErrorWithStatusCode(
		errors.New("busy"),
		types.ErrorCodeChannelConcurrencyLimitExceeded,
		http.StatusTooManyRequests,
		types.ErrOptionWithNoRecordErrorLog(),
	)

	if !types.IsChannelConcurrencyLimitExceeded(err) {
		t.Fatal("expected concurrency limit error")
	}
	if ShouldDisableChannel(err) {
		t.Fatal("concurrency limit should not disable channel")
	}
	if types.IsRecordErrorLog(err) {
		t.Fatal("concurrency limit should not record error log")
	}
}

func TestCacheGetRandomSatisfiedChannelTracksOneLease(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const channelID = 930001
	const modelName = "responses-lease-model"
	createChannelSelectAutoGroupsChannel(t, db, channelID, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	retry := 0

	channel, _, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  modelName,
		Retry:      &retry,
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	assert.Equal(t, channelID, channel.Id)
	require.Equal(t, 1, model.GetChannelCurrentConcurrency(channelID))

	ReleaseChannelConcurrencyLease(ctx)
	assert.Zero(t, model.GetChannelCurrentConcurrency(channelID))
}

func TestSelectChannelForRequestTracksPinnedLease(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const channelID = 930002
	const modelName = "responses-pinned-lease-model"
	createChannelSelectAutoGroupsChannel(t, db, channelID, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	GetChannelConstraints(ctx).AddPin(appdto.ChannelPin{
		ChannelId: channelID,
		Source:    appdto.PinSourceToken,
		Rank:      appdto.PinRankToken,
		RetryMode: appdto.PinRetrySingleAttempt,
	})
	retry := 0

	channel, _, selectErr := SelectChannelForRequest(ctx, modelName, &RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  modelName,
		Retry:      &retry,
	})
	require.Nil(t, selectErr)
	require.NotNil(t, channel)
	assert.Equal(t, channelID, channel.Id)
	require.Equal(t, 1, model.GetChannelCurrentConcurrency(channelID))

	ReleaseChannelConcurrencyLease(ctx)
	assert.Zero(t, model.GetChannelCurrentConcurrency(channelID))
}

func TestSelectChannelForRequestTracksAffinityLease(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const channelID = 930003
	const modelName = "responses-affinity-lease-model"
	const affinityValue = "responses-affinity-lease-key"
	createChannelSelectAutoGroupsChannel(t, db, channelID, "default", modelName)
	model.InitChannelCache()

	rule := operation_setting.ChannelAffinityRule{
		Name:       "concurrency-lease-test",
		ModelRegex: []string{"^responses-affinity-lease-model$"},
		PathRegex:  []string{"/v1/responses"},
		KeySources: []operation_setting.ChannelAffinityKeySource{
			{Type: "request_header", Key: "X-Affinity-Key"},
		},
	}
	cacheKey := buildChannelAffinityCacheKeySuffix(rule, modelName, "default", affinityValue)
	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(cacheKey, channelID, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKey})
	})

	affinitySetting := operation_setting.GetChannelAffinitySetting()
	originalEnabled, originalRules := affinitySetting.Enabled, affinitySetting.Rules
	affinitySetting.Enabled = true
	affinitySetting.Rules = append([]operation_setting.ChannelAffinityRule{rule}, originalRules...)
	t.Cleanup(func() {
		affinitySetting.Enabled = originalEnabled
		affinitySetting.Rules = originalRules
	})

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("X-Affinity-Key", affinityValue)
	retry := 0

	channel, _, selectErr := SelectChannelForRequest(ctx, modelName, &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "default",
		ModelName:   modelName,
		RequestPath: "/v1/responses",
		Retry:       &retry,
	})
	require.Nil(t, selectErr)
	require.NotNil(t, channel)
	assert.Equal(t, channelID, channel.Id)
	require.Equal(t, 1, model.GetChannelCurrentConcurrency(channelID))

	ReleaseChannelConcurrencyLease(ctx)
	assert.Zero(t, model.GetChannelCurrentConcurrency(channelID))
}
