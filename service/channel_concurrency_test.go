package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
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
