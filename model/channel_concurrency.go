package model

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
)

const DefaultChannelConcurrencyLimit = 10

type ChannelConcurrencyFullError struct {
	ChannelId   int
	ChannelName string
	Current     int
	Limit       int
}

func (e *ChannelConcurrencyFullError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("渠道「%s」（#%d）并发已满：%d/%d", e.ChannelName, e.ChannelId, e.Current, e.Limit)
}

func IsChannelConcurrencyFullError(err error) bool {
	var fullErr *ChannelConcurrencyFullError
	return errors.As(err, &fullErr)
}

var (
	channelConcurrencyLock  sync.Mutex
	channelConcurrencyUsage = make(map[int]int)
)

func NormalizeChannelConcurrencyLimit(limit int) int {
	if limit <= 0 {
		return DefaultChannelConcurrencyLimit
	}
	return limit
}

func (channel *Channel) GetConcurrencyLimit() int {
	if channel == nil || channel.ConcurrencyLimit == nil {
		return DefaultChannelConcurrencyLimit
	}
	return NormalizeChannelConcurrencyLimit(*channel.ConcurrencyLimit)
}

func NewChannelConcurrencyFullError(channel *Channel, current int, limit int) error {
	if channel == nil {
		return &ChannelConcurrencyFullError{Current: current, Limit: limit}
	}
	return &ChannelConcurrencyFullError{
		ChannelId:   channel.Id,
		ChannelName: channel.Name,
		Current:     current,
		Limit:       limit,
	}
}

func GetChannelCurrentConcurrency(channelId int) int {
	if channelId <= 0 {
		return 0
	}
	channelConcurrencyLock.Lock()
	defer channelConcurrencyLock.Unlock()
	return channelConcurrencyUsage[channelId]
}

func GetChannelConcurrencySnapshot(channel *Channel) (current int, limit int) {
	if channel == nil {
		return 0, DefaultChannelConcurrencyLimit
	}
	limit = channel.GetConcurrencyLimit()
	channelConcurrencyLock.Lock()
	defer channelConcurrencyLock.Unlock()
	return channelConcurrencyUsage[channel.Id], limit
}

func IsChannelConcurrencyFull(channel *Channel) bool {
	current, limit := GetChannelConcurrencySnapshot(channel)
	return current >= limit
}

func TryAcquireChannelConcurrency(channel *Channel) (current int, limit int, ok bool) {
	if channel == nil || channel.Id <= 0 {
		return 0, DefaultChannelConcurrencyLimit, false
	}
	limit = channel.GetConcurrencyLimit()
	channelConcurrencyLock.Lock()
	defer channelConcurrencyLock.Unlock()
	current = channelConcurrencyUsage[channel.Id]
	if current >= limit {
		return current, limit, false
	}
	current++
	channelConcurrencyUsage[channel.Id] = current
	return current, limit, true
}

func ReleaseChannelConcurrency(channelId int) {
	if channelId <= 0 {
		return
	}
	channelConcurrencyLock.Lock()
	defer channelConcurrencyLock.Unlock()
	current := channelConcurrencyUsage[channelId]
	if current <= 1 {
		delete(channelConcurrencyUsage, channelId)
		return
	}
	channelConcurrencyUsage[channelId] = current - 1
}

func AttachChannelConcurrency(channel *Channel) {
	if channel == nil {
		return
	}
	limit := channel.GetConcurrencyLimit()
	channel.ConcurrencyLimit = &limit
	channel.CurrentConcurrency = GetChannelCurrentConcurrency(channel.Id)
}

func AttachChannelsConcurrency(channels []*Channel) {
	for _, channel := range channels {
		AttachChannelConcurrency(channel)
	}
}

func pickAvailableChannelByWeight(channels []*Channel) (*Channel, error) {
	candidates := make([]*Channel, 0, len(channels))
	for _, channel := range channels {
		if channel != nil {
			candidates = append(candidates, channel)
		}
	}

	var lastFullErr error
	for len(candidates) > 0 {
		available := make([]*Channel, 0, len(candidates))
		sumWeight := 0
		for _, channel := range candidates {
			current, limit := GetChannelConcurrencySnapshot(channel)
			if current >= limit {
				lastFullErr = NewChannelConcurrencyFullError(channel, current, limit)
				continue
			}
			available = append(available, channel)
			sumWeight += channel.GetWeight()
		}
		if len(available) == 0 {
			if lastFullErr != nil {
				return nil, lastFullErr
			}
			return nil, nil
		}

		selectedIndex := weightedChannelIndex(available, sumWeight)
		selected := available[selectedIndex]
		current, limit, ok := TryAcquireChannelConcurrency(selected)
		if ok {
			return selected, nil
		}
		lastFullErr = NewChannelConcurrencyFullError(selected, current, limit)

		nextCandidates := candidates[:0]
		for _, channel := range candidates {
			if channel.Id != selected.Id {
				nextCandidates = append(nextCandidates, channel)
			}
		}
		candidates = nextCandidates
	}
	return nil, lastFullErr
}

func weightedChannelIndex(channels []*Channel, sumWeight int) int {
	if len(channels) <= 1 {
		return 0
	}

	smoothingFactor := 1
	smoothingAdjustment := 0
	if sumWeight == 0 {
		sumWeight = len(channels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(channels) < 10 {
		smoothingFactor = 100
	}

	totalWeight := sumWeight * smoothingFactor
	randomWeight := rand.Intn(totalWeight)
	for index, channel := range channels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return index
		}
	}
	return len(channels) - 1
}
