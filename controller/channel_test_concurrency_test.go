package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func TestChannelTestReleasesConcurrencyLease(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channelId := 42001
	ctx, _ := gin.CreateTestContext(nil)
	service.TrackChannelConcurrencyLease(ctx, channelId)

	if current := model.GetChannelCurrentConcurrency(channelId); current != 0 {
		t.Fatalf("tracking a lease should not acquire counter, got %d", current)
	}

	if _, _, ok := model.TryAcquireChannelConcurrency(&model.Channel{Id: channelId, Name: "test"}); !ok {
		t.Fatal("setup acquire should succeed")
	}
	service.ReleaseChannelConcurrencyLease(ctx)

	if current := model.GetChannelCurrentConcurrency(channelId); current != 0 {
		t.Fatalf("release should clear channel test concurrency, got %d", current)
	}
}
