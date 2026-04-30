package service

import (
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const channelConcurrencyLeaseKey = "channel_concurrency_lease_id"

func AcquireChannelConcurrencyLease(c *gin.Context, channel *model.Channel) error {
	if channel == nil {
		return nil
	}
	if HasChannelConcurrencyLease(c, channel.Id) {
		return nil
	}
	current, limit, ok := model.TryAcquireChannelConcurrency(channel)
	if !ok {
		return model.NewChannelConcurrencyFullError(channel, current, limit)
	}
	TrackChannelConcurrencyLease(c, channel.Id)
	return nil
}

func TrackChannelConcurrencyLease(c *gin.Context, channelId int) {
	if c == nil || channelId <= 0 {
		return
	}
	releaseChannelConcurrencyLease(c)
	c.Set(channelConcurrencyLeaseKey, channelId)
}

func HasChannelConcurrencyLease(c *gin.Context, channelId int) bool {
	if c == nil || channelId <= 0 {
		return false
	}
	existing, ok := c.Get(channelConcurrencyLeaseKey)
	if !ok {
		return false
	}
	existingId, ok := existing.(int)
	return ok && existingId == channelId
}

func ReleaseChannelConcurrencyLease(c *gin.Context) {
	if c == nil {
		return
	}
	releaseChannelConcurrencyLease(c)
}

func releaseChannelConcurrencyLease(c *gin.Context) {
	existing, ok := c.Get(channelConcurrencyLeaseKey)
	if !ok {
		return
	}
	if existingId, ok := existing.(int); ok && existingId > 0 {
		model.ReleaseChannelConcurrency(existingId)
	}
	if c.Keys != nil {
		delete(c.Keys, channelConcurrencyLeaseKey)
	}
}
