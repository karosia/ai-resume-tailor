package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// Client tries providers in order and returns the first success.
// Order = priority: NewClient(anthropic, openai) means Anthropic first.
type Client struct {
	providers []Provider
	log       *slog.Logger
}

func NewClient(log *slog.Logger, providers ...Provider) (*Client, error) {
	if len(providers) == 0 {
		return nil, errors.New("llm: NewClient requires at least one provider")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{providers: providers, log: log}, nil
}

func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	var errs []error
	for _, p := range c.providers {
		if err := ctx.Err(); err != nil {
			return nil, err // caller gave up; stop trying
		}

		resp, err := p.Complete(ctx, req)
		if err != nil {
			c.log.Warn("llm provider failed, trying next",
				"provider", p.Name(), "error", err)
			errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
			continue
		}

		if len(errs) > 0 {
			c.log.Info("llm succeeded via fallback",
				"provider", p.Name(), "prior_failures", len(errs))
		}
		return resp, nil
	}

	return nil, fmt.Errorf("llm: all %d providers failed: %w",
		len(c.providers), errors.Join(errs...))
}
