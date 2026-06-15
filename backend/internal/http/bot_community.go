package httpapi

import (
	"context"
	"errors"

	"git.subcult.tv/PatrickFanella/over-laid/internal/bot"
)

var errBotStoreUnavailable = errors.New("bot storage unavailable")

type botCommunityAdapter struct {
	store *bot.Store
}

func (a botCommunityAdapter) IncrementCounter(name string, delta int) (bot.Counter, error) {
	if a.store == nil {
		return bot.Counter{}, errBotStoreUnavailable
	}
	return a.store.IncrementCounter(context.Background(), name, delta)
}

func (a botCommunityAdapter) CounterValue(name string) (bot.Counter, bool, error) {
	if a.store == nil {
		return bot.Counter{}, false, errBotStoreUnavailable
	}
	return a.store.CounterValue(context.Background(), name)
}

func (a botCommunityAdapter) RandomQuote() (bot.Quote, bool, error) {
	if a.store == nil {
		return bot.Quote{}, false, errBotStoreUnavailable
	}
	return a.store.RandomQuote(context.Background())
}

func (a botCommunityAdapter) AddQuote(text string, author string) (bot.Quote, error) {
	if a.store == nil {
		return bot.Quote{}, errBotStoreUnavailable
	}
	return a.store.SaveQuote(context.Background(), bot.Quote{Text: text, Author: author})
}

func (a botCommunityAdapter) DeleteQuote(id string) error {
	if a.store == nil {
		return errBotStoreUnavailable
	}
	return a.store.DeleteQuote(context.Background(), id)
}
