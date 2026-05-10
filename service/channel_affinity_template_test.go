package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func buildChannelAffinityTemplateContextForTest(meta channelAffinityMeta) *gin.Context {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	setChannelAffinityContext(ctx, meta)
	return ctx
}

func TestApplyChannelAffinityOverrideTemplate_NoTemplate(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-no-template",
	})
	base := map[string]interface{}{
		"temperature": 0.7,
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.False(t, applied)
	require.Equal(t, base, merged)
}

func TestApplyChannelAffinityOverrideTemplate_MergeTemplate(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-with-template",
		ParamTemplate: map[string]interface{}{
			"temperature": 0.2,
			"top_p":       0.95,
		},
		UsingGroup:     "default",
		ModelName:      "gpt-4.1",
		RequestPath:    "/v1/responses",
		KeySourceType:  "gjson",
		KeySourcePath:  "prompt_cache_key",
		KeyHint:        "abcd...wxyz",
		KeyFingerprint: "abcd1234",
	})
	base := map[string]interface{}{
		"temperature": 0.7,
		"max_tokens":  2000,
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.True(t, applied)
	require.Equal(t, 0.7, merged["temperature"])
	require.Equal(t, 0.95, merged["top_p"])
	require.Equal(t, 2000, merged["max_tokens"])
	require.Equal(t, 0.7, base["temperature"])

	anyInfo, ok := ctx.Get(ginKeyChannelAffinityLogInfo)
	require.True(t, ok)
	info, ok := anyInfo.(map[string]interface{})
	require.True(t, ok)
	overrideInfoAny, ok := info["override_template"]
	require.True(t, ok)
	overrideInfo, ok := overrideInfoAny.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, overrideInfo["applied"])
	require.Equal(t, "rule-with-template", overrideInfo["rule_name"])
	require.EqualValues(t, 2, overrideInfo["param_override_keys"])
}

func TestApplyChannelAffinityOverrideTemplate_MergeOperations(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-with-ops-template",
		ParamTemplate: map[string]interface{}{
			"operations": []map[string]interface{}{
				{
					"mode":  "pass_headers",
					"value": []string{"Originator"},
				},
			},
		},
	})
	base := map[string]interface{}{
		"temperature": 0.7,
		"operations": []map[string]interface{}{
			{
				"path":  "model",
				"mode":  "trim_prefix",
				"value": "openai/",
			},
		},
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.True(t, applied)
	require.Equal(t, 0.7, merged["temperature"])

	opsAny, ok := merged["operations"]
	require.True(t, ok)
	ops, ok := opsAny.([]interface{})
	require.True(t, ok)
	require.Len(t, ops, 2)

	firstOp, ok := ops[0].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "pass_headers", firstOp["mode"])

	secondOp, ok := ops[1].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "trim_prefix", secondOp["mode"])
}

func TestShouldSkipRetryAfterChannelAffinityFailure(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() *gin.Context
		want bool
	}{
		{
			name: "nil context",
			ctx: func() *gin.Context {
				return nil
			},
			want: false,
		},
		{
			name: "explicit skip retry flag in context",
			ctx: func() *gin.Context {
				ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-explicit-flag",
					SkipRetry:  false,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
				ctx.Set(ginKeyChannelAffinitySkipRetry, true)
				return ctx
			},
			want: true,
		},
		{
			name: "fallback to matched rule meta",
			ctx: func() *gin.Context {
				return buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-skip-retry",
					SkipRetry:  true,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
			},
			want: true,
		},
		{
			name: "no flag and no skip retry meta",
			ctx: func() *gin.Context {
				return buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-no-skip-retry",
					SkipRetry:  false,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ShouldSkipRetryAfterChannelAffinityFailure(tt.ctx()))
		})
	}
}

func TestClearCurrentChannelAffinityCacheDeletesKeyAndForcesSuccessSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	originalSwitchOnSuccess := setting.SwitchOnSuccess
	setting.SwitchOnSuccess = false
	t.Cleanup(func() {
		setting.SwitchOnSuccess = originalSwitchOnSuccess
	})

	cache := getChannelAffinityCache()
	cacheKeySuffix := fmt.Sprintf("stale-rule:default:stale-%d", time.Now().UnixNano())
	require.NoError(t, cache.SetWithTTL(cacheKeySuffix, 11, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKeySuffix})
	})

	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		CacheKey:   cache.FullKey(cacheKeySuffix),
		TTLSeconds: 60,
		RuleName:   "stale-rule",
		UsingGroup: "default",
		ModelName:  "gpt-5",
	})

	require.Equal(t, 1, ClearCurrentChannelAffinityCache(ctx))
	_, found, err := cache.Get(cacheKeySuffix)
	require.NoError(t, err)
	require.False(t, found)

	ctx.Set("channel_id", 22)
	RecordChannelAffinity(ctx, 11)
	channelID, found, err := cache.Get(cacheKeySuffix)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 22, channelID)
}

func TestRecordChannelAffinityKeepsOriginalBindingWhenSwitchOnSuccessDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	originalSwitchOnSuccess := setting.SwitchOnSuccess
	setting.SwitchOnSuccess = false
	t.Cleanup(func() {
		setting.SwitchOnSuccess = originalSwitchOnSuccess
	})

	cache := getChannelAffinityCache()
	cacheKeySuffix := fmt.Sprintf("transient-rule:default:transient-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKeySuffix})
	})

	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		CacheKey:   cache.FullKey(cacheKeySuffix),
		TTLSeconds: 60,
		RuleName:   "transient-rule",
		UsingGroup: "default",
		ModelName:  "gpt-5",
	})
	ctx.Set("channel_id", 22)

	RecordChannelAffinity(ctx, 11)

	channelID, found, err := cache.Get(cacheKeySuffix)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 11, channelID)
}

func TestChannelAffinityRecoveryClearPrefixesDropsModelSegmentWhenRuleIncludesModel(t *testing.T) {
	rules := []operation_setting.ChannelAffinityRule{
		{
			Name:              "rule-a",
			ModelRegex:        []string{"^gpt-.*$"},
			IncludeRuleName:   true,
			IncludeModelName:  true,
			IncludeUsingGroup: true,
		},
		{
			Name:              "rule-b",
			ModelRegex:        []string{"^claude-.*$"},
			IncludeRuleName:   true,
			IncludeModelName:  true,
			IncludeUsingGroup: true,
		},
	}
	channel := &model.Channel{
		Id:     123,
		Models: "gpt-5,claude-3",
		Group:  "default,pro",
	}

	prefixes, fullClear := channelAffinityRecoveryClearPrefixes(rules, channel)

	require.False(t, fullClear)
	require.ElementsMatch(t, []string{
		"rule-a",
		"rule-b",
	}, prefixes)
}

func TestClearChannelAffinityCacheForRecoveredChannelClearsRecoveredRulePrefixesWithoutModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	originalEnabled := setting.Enabled
	originalRules := setting.Rules
	setting.Enabled = true
	setting.Rules = []operation_setting.ChannelAffinityRule{
		{
			Name:              "recovered-rule",
			ModelRegex:        []string{"^gpt-.*$"},
			IncludeRuleName:   true,
			IncludeModelName:  true,
			IncludeUsingGroup: true,
		},
		{
			Name:              "other-rule",
			ModelRegex:        []string{"^claude-.*$"},
			IncludeRuleName:   true,
			IncludeModelName:  true,
			IncludeUsingGroup: true,
		},
	}
	t.Cleanup(func() {
		setting.Enabled = originalEnabled
		setting.Rules = originalRules
	})

	cache := getChannelAffinityCache()
	keys := []string{
		"recovered-rule:gpt-5:default:key1",
		"recovered-rule:gpt-5:pro:key2",
		"recovered-rule:gpt-4:default:key3",
		"other-rule:claude-3:default:key4",
		"recovered-rule:gpt-5:auto:key5",
		"recovered-rule:gemini-2.5-pro-thinking-8192:default:key6",
	}
	for i, key := range keys {
		require.NoError(t, cache.SetWithTTL(key, i+1, time.Minute))
	}
	t.Cleanup(func() {
		_, _ = cache.DeleteMany(keys)
	})

	deleted := ClearChannelAffinityCacheForRecoveredChannel(&model.Channel{
		Id:     321,
		Models: "gpt-5,gemini-2.5-pro-thinking-*",
		Group:  "default",
	})

	require.Equal(t, 5, deleted)
	var found bool
	var err error
	for _, key := range []string{
		"recovered-rule:gpt-5:default:key1",
		"recovered-rule:gpt-5:pro:key2",
		"recovered-rule:gpt-4:default:key3",
		"recovered-rule:gpt-5:auto:key5",
		"recovered-rule:gemini-2.5-pro-thinking-8192:default:key6",
	} {
		_, found, err = cache.Get(key)
		require.NoError(t, err)
		require.False(t, found, key)
	}

	_, found, err = cache.Get("other-rule:claude-3:default:key4")
	require.NoError(t, err)
	require.True(t, found)
}

func TestChannelAffinityHitCodexTemplatePassHeadersEffective(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)

	var codexRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		rule := &setting.Rules[i]
		if strings.EqualFold(strings.TrimSpace(rule.Name), "codex cli trace") {
			codexRule = rule
			break
		}
	}
	require.NotNil(t, codexRule)

	affinityValue := fmt.Sprintf("pc-hit-%d", time.Now().UnixNano())
	cacheKeySuffix := buildChannelAffinityCacheKeySuffix(*codexRule, "gpt-5", "default", affinityValue)

	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(cacheKeySuffix, 9527, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKeySuffix})
	})

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"prompt_cache_key":"%s"}`, affinityValue)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	channelID, found := GetPreferredChannelByAffinity(ctx, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 9527, channelID)

	baseOverride := map[string]interface{}{
		"temperature": 0.2,
	}
	mergedOverride, applied := ApplyChannelAffinityOverrideTemplate(ctx, baseOverride)
	require.True(t, applied)
	require.Equal(t, 0.2, mergedOverride["temperature"])

	info := &relaycommon.RelayInfo{
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
			"User-Agent": "codex-cli-test",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: mergedOverride,
			HeadersOverride: map[string]interface{}{
				"X-Static": "legacy-static",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-5"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)

	require.Equal(t, "legacy-static", info.RuntimeHeadersOverride["x-static"])
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	require.Equal(t, "codex-cli-test", info.RuntimeHeadersOverride["user-agent"])

	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	_, exists = info.RuntimeHeadersOverride["x-codex-turn-metadata"]
	require.False(t, exists)
}
