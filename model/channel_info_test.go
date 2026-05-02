package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

func TestChannelInfoValueReturnsJSONString(t *testing.T) {
	value, err := (ChannelInfo{
		IsMultiKey:           false,
		MultiKeySize:         0,
		MultiKeyPollingIndex: 0,
		MultiKeyMode:         constant.MultiKeyModeRandom,
	}).Value()
	if err != nil {
		t.Fatalf("ChannelInfo.Value returned error: %v", err)
	}

	text, ok := value.(string)
	if !ok {
		t.Fatalf("ChannelInfo.Value should return string for PostgreSQL json columns, got %T", value)
	}
	if text == "" || text[0] != '{' {
		t.Fatalf("ChannelInfo.Value returned invalid JSON object text: %q", text)
	}
}

func TestChannelInfoScanAcceptsString(t *testing.T) {
	var info ChannelInfo
	err := info.Scan(`{"is_multi_key":true,"multi_key_size":2,"multi_key_status_list":{"1":2},"multi_key_polling_index":1,"multi_key_mode":"polling"}`)
	if err != nil {
		t.Fatalf("ChannelInfo.Scan returned error: %v", err)
	}
	if !info.IsMultiKey || info.MultiKeySize != 2 || info.MultiKeyStatusList[1] != 2 || info.MultiKeyMode != constant.MultiKeyModePolling {
		t.Fatalf("unexpected scanned ChannelInfo: %#v", info)
	}
}
