package twitch

import (
	"strings"
	"sync"
	"time"

	"git.subcult.tv/PatrickFanella/over-laid/internal/interactions"
)

type rewardCooldownKey struct{ broadcaster, reward string }
type rewardCooldown struct{ last, expires time.Time }

// The client retains this state across reconnects; other clients never share it.
type channelPointProcessor struct {
	mu        sync.Mutex
	now       func() time.Time
	cooldowns map[rewardCooldownKey]rewardCooldown
}

func (p *channelPointProcessor) coolingDown(rule interactions.ChannelPointRule, event map[string]any) bool {
	if rule.CooldownSeconds <= 0 {
		return false
	}
	reward := nestedMap(event, "reward")
	identity := stringValue(reward, "id")
	if identity == "" {
		identity = "title:" + strings.ToLower(strings.TrimSpace(stringValue(reward, "title")))
	} else {
		identity = "id:" + identity
	}
	key := rewardCooldownKey{stringValue(event, "broadcaster_user_id"), identity}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	if p.now != nil {
		now = p.now().UTC()
	}
	if p.cooldowns == nil {
		p.cooldowns = make(map[rewardCooldownKey]rewardCooldown)
	}
	// Remove expired entries so deleted/renamed rewards do not accumulate.
	for existingKey, state := range p.cooldowns {
		if existingKey != key && !now.Before(state.expires) {
			delete(p.cooldowns, existingKey)
		}
	}
	interval := time.Duration(rule.CooldownSeconds) * time.Second
	if state, ok := p.cooldowns[key]; ok && now.Sub(state.last) < interval {
		state.expires = state.last.Add(interval)
		p.cooldowns[key] = state
		return true
	}
	p.cooldowns[key] = rewardCooldown{last: now, expires: now.Add(interval)}
	return false
}
