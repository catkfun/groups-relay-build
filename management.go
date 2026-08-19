package main

import (
	"context"
	"net"
	"sync"

	"github.com/nbd-wtf/go-nostr/nip86"
)

// 内存态 NIP-86 中继管理实现：
// 记录封禁/放行 pubkey、事件、kind、IP，供 ManagementAPI 各方法使用。
// 状态仅存内存，relay 重启后清空（如需持久化可扩展为 SQLite/LMDB）。
type mgmtState struct {
	mu           sync.RWMutex
	bannedPubs   map[string]string
	allowedPubs  map[string]string
	bannedEvents map[string]string
	allowedKinds map[int]string
	allowedEvts  map[string]string
	blockedIPs   map[string]string
}

var mgmt = &mgmtState{
	bannedPubs:   make(map[string]string),
	allowedPubs:  make(map[string]string),
	bannedEvents: make(map[string]string),
	allowedKinds: make(map[int]string),
	allowedEvts:  make(map[string]string),
	blockedIPs:   make(map[string]string),
}

func (m *mgmtState) pubs(src map[string]string) []nip86.PubKeyReason {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]nip86.PubKeyReason, 0, len(src))
	for pk, reason := range src {
		out = append(out, nip86.PubKeyReason{PubKey: pk, Reason: reason})
	}
	return out
}

func (m *mgmtState) ids(src map[string]string) []nip86.IDReason {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]nip86.IDReason, 0, len(src))
	for id, reason := range src {
		out = append(out, nip86.IDReason{ID: id, Reason: reason})
	}
	return out
}

func (m *mgmtState) ips() []nip86.IPReason {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]nip86.IPReason, 0, len(m.blockedIPs))
	for ip, reason := range m.blockedIPs {
		out = append(out, nip86.IPReason{IP: ip, Reason: reason})
	}
	return out
}

func (m *mgmtState) kinds() []int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]int, 0, len(m.allowedKinds))
	for k := range m.allowedKinds {
		out = append(out, k)
	}
	return out
}

func (m *mgmtState) banPub(pk, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bannedPubs[pk] = reason
	delete(m.allowedPubs, pk)
}
func (m *mgmtState) unbanPub(pk string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.bannedPubs, pk)
}
func (m *mgmtState) allowPub(pk, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowedPubs[pk] = reason
	delete(m.bannedPubs, pk)
}
func (m *mgmtState) unallowPub(pk string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.allowedPubs, pk)
}
func (m *mgmtState) banEvt(id, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bannedEvents[id] = reason
	delete(m.allowedEvts, id)
}
func (m *mgmtState) allowEvt(id, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowedEvts[id] = reason
	delete(m.bannedEvents, id)
}
func (m *mgmtState) allowKind(k int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowedKinds[k] = ""
}
func (m *mgmtState) disallowKind(k int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.allowedKinds, k)
}
func (m *mgmtState) blockIP(ip, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blockedIPs[ip] = reason
}
func (m *mgmtState) unblockIP(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.blockedIPs, ip)
}

// installNIP86Management 填充 khatru ManagementAPI 各方法，使其返回有效结果而非 "not supported"。
func installNIP86Management() {
	relay.ManagementAPI.BanPubKey = func(ctx context.Context, pk, reason string) error {
		mgmt.banPub(pk, reason)
		return nil
	}
	relay.ManagementAPI.AllowPubKey = func(ctx context.Context, pk, reason string) error {
		mgmt.allowPub(pk, reason)
		return nil
	}
	relay.ManagementAPI.ListBannedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) {
		return mgmt.pubs(mgmt.bannedPubs), nil
	}
	relay.ManagementAPI.ListAllowedPubKeys = func(ctx context.Context) ([]nip86.PubKeyReason, error) {
		return mgmt.pubs(mgmt.allowedPubs), nil
	}
	relay.ManagementAPI.BanEvent = func(ctx context.Context, id, reason string) error {
		mgmt.banEvt(id, reason)
		return nil
	}
	relay.ManagementAPI.AllowEvent = func(ctx context.Context, id, reason string) error {
		mgmt.allowEvt(id, reason)
		return nil
	}
	relay.ManagementAPI.ListBannedEvents = func(ctx context.Context) ([]nip86.IDReason, error) {
		return mgmt.ids(mgmt.bannedEvents), nil
	}
	relay.ManagementAPI.ListAllowedEvents = func(ctx context.Context) ([]nip86.IDReason, error) {
		return mgmt.ids(mgmt.allowedEvts), nil
	}
	relay.ManagementAPI.ListEventsNeedingModeration = func(ctx context.Context) ([]nip86.IDReason, error) {
		return []nip86.IDReason{}, nil
	}
	relay.ManagementAPI.AllowKind = func(ctx context.Context, kind int) error {
		mgmt.allowKind(kind)
		return nil
	}
	relay.ManagementAPI.DisallowKind = func(ctx context.Context, kind int) error {
		mgmt.disallowKind(kind)
		return nil
	}
	relay.ManagementAPI.ListAllowedKinds = func(ctx context.Context) ([]int, error) {
		return mgmt.kinds(), nil
	}
	relay.ManagementAPI.ListDisAllowedKinds = func(ctx context.Context) ([]int, error) {
		return []int{}, nil
	}
	relay.ManagementAPI.BlockIP = func(ctx context.Context, ip net.IP, reason string) error {
		mgmt.blockIP(ip.String(), reason)
		return nil
	}
	relay.ManagementAPI.UnblockIP = func(ctx context.Context, ip net.IP, reason string) error {
		mgmt.unblockIP(ip.String())
		return nil
	}
	relay.ManagementAPI.ListBlockedIPs = func(ctx context.Context) ([]nip86.IPReason, error) {
		return mgmt.ips(), nil
	}
	relay.ManagementAPI.ChangeRelayName = func(ctx context.Context, name string) error {
		relay.Info.Name = name
		return nil
	}
	relay.ManagementAPI.ChangeRelayDescription = func(ctx context.Context, desc string) error {
		relay.Info.Description = desc
		return nil
	}
	relay.ManagementAPI.ChangeRelayIcon = func(ctx context.Context, icon string) error {
		relay.Info.Icon = icon
		return nil
	}
	relay.ManagementAPI.Stats = func(ctx context.Context) (nip86.Response, error) {
		return nip86.Response{}, nil
	}
}