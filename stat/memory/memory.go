package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/conf"
	"github.com/p4gefau1t/trojan-go/stat"
)

type MemoryTrafficMeter struct {
	stat.TrafficMeter

	sent     uint64
	recv     uint64
	lastSent uint64
	lastRecv uint64
	hash     string
	ctx      context.Context
	cancel   context.CancelFunc
}

func (m *MemoryTrafficMeter) Close() error {
	m.Reset()
	m.cancel()
	return nil
}

func (m *MemoryTrafficMeter) Count(sent, recv uint64) {
	atomic.AddUint64(&m.sent, uint64(sent))
	atomic.AddUint64(&m.recv, uint64(recv))
}

func (m *MemoryTrafficMeter) Hash() string {
	return m.hash
}

func (m *MemoryTrafficMeter) Get() (uint64, uint64) {
	return atomic.LoadUint64(&m.sent), atomic.LoadUint64(&m.recv)
}

func (m *MemoryTrafficMeter) Reset() {
	atomic.StoreUint64(&m.sent, 0)
	atomic.StoreUint64(&m.recv, 0)
	atomic.StoreUint64(&m.lastSent, 0)
	atomic.StoreUint64(&m.lastRecv, 0)
}

func (m *MemoryTrafficMeter) GetAndReset() (uint64, uint64) {
	sent := atomic.SwapUint64(&m.sent, 0)
	recv := atomic.SwapUint64(&m.recv, 0)
	atomic.StoreUint64(&m.lastSent, 0)
	atomic.StoreUint64(&m.lastRecv, 0)
	return sent, recv
}

func (m *MemoryTrafficMeter) speedUpdater() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(time.Second):
			lastSent, lastRecv := m.Get()
			atomic.StoreUint64(&m.lastSent, lastSent)
			atomic.StoreUint64(&m.lastRecv, lastRecv)
		}
	}

}

func (m *MemoryTrafficMeter) GetSpeed() (uint64, uint64) {
	sent, recv := m.Get()
	lastSent := atomic.LoadUint64(&m.lastSent)
	lastRecv := atomic.LoadUint64(&m.lastRecv)
	return sent - lastSent, recv - lastRecv
}

type MemoryAuthenticator struct {
	stat.Authenticator
	sync.RWMutex

	users map[string]*MemoryTrafficMeter
	ctx   context.Context
}

func (a *MemoryAuthenticator) AuthUser(hash string) (bool, stat.TrafficMeter) {
	a.RLock()
	defer a.RUnlock()
	if user, found := a.users[hash]; found {
		return true, user
	}
	return false, nil
}

func (a *MemoryAuthenticator) AddUser(hash string) error {
	a.Lock()
	defer a.Unlock()
	if _, found := a.users[hash]; found {
		return common.NewError("hash " + hash + " is already exist")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	meter := &MemoryTrafficMeter{
		hash:   hash,
		ctx:    ctx,
		cancel: cancel,
	}
	go meter.speedUpdater()
	a.users[hash] = meter
	return nil
}

func (a *MemoryAuthenticator) DelUser(hash string) error {
	a.Lock()
	defer a.Unlock()
	meter, found := a.users[hash]
	if !found {
		return common.NewError("hash " + hash + "is not exist")
	}
	meter.Close()
	delete(a.users, hash)
	return nil
}

func (a *MemoryAuthenticator) ListUsers() []stat.TrafficMeter {
	a.RLock()
	defer a.RUnlock()
	result := make([]stat.TrafficMeter, 0, len(a.users))
	for _, m := range a.users {
		result = append(result, m)
	}
	return result
}

func NewMemoryAuth(ctx context.Context, config *conf.GlobalConfig) (stat.Authenticator, error) {
	a := &MemoryAuthenticator{
		ctx:   ctx,
		users: make(map[string]*MemoryTrafficMeter),
	}
	for hash := range config.Hash {
		a.AddUser(hash)
	}
	return a, nil
}

func init() {
	stat.RegisterAuthCreator("memory", NewMemoryAuth)
}
