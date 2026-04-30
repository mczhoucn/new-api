package model

import "testing"

func resetChannelConcurrencyForTest() {
	channelConcurrencyLock.Lock()
	defer channelConcurrencyLock.Unlock()
	channelConcurrencyUsage = make(map[int]int)
}

func TestChannelConcurrencyAcquireRelease(t *testing.T) {
	resetChannelConcurrencyForTest()
	limit := 1
	channel := &Channel{Id: 1, Name: "test", ConcurrencyLimit: &limit}

	current, gotLimit, ok := TryAcquireChannelConcurrency(channel)
	if !ok {
		t.Fatal("first acquire should succeed")
	}
	if current != 1 || gotLimit != limit {
		t.Fatalf("unexpected acquire snapshot: current=%d limit=%d", current, gotLimit)
	}

	current, gotLimit, ok = TryAcquireChannelConcurrency(channel)
	if ok {
		t.Fatal("second acquire should fail at limit")
	}
	if current != 1 || gotLimit != limit {
		t.Fatalf("unexpected full snapshot: current=%d limit=%d", current, gotLimit)
	}

	ReleaseChannelConcurrency(channel.Id)
	if current := GetChannelCurrentConcurrency(channel.Id); current != 0 {
		t.Fatalf("release should clear usage, got %d", current)
	}
}

func TestPickAvailableChannelByWeightSkipsFullCandidate(t *testing.T) {
	resetChannelConcurrencyForTest()
	limit := 1
	full := &Channel{Id: 1, Name: "full", ConcurrencyLimit: &limit}
	available := &Channel{Id: 2, Name: "available", ConcurrencyLimit: &limit}

	if _, _, ok := TryAcquireChannelConcurrency(full); !ok {
		t.Fatal("setup acquire should succeed")
	}

	selected, err := pickAvailableChannelByWeight([]*Channel{full, available})
	if err != nil {
		t.Fatalf("pick should skip full channel: %v", err)
	}
	if selected == nil || selected.Id != available.Id {
		t.Fatalf("expected available channel, got %#v", selected)
	}
	if current := GetChannelCurrentConcurrency(available.Id); current != 1 {
		t.Fatalf("selected channel should be acquired, got %d", current)
	}
}
