package httpapi

import "github.com/lorehub/lorehub/services/api/internal/webhooks"

type webhooksManager = webhooks.Manager

func WithWebhooks(store webhooks.Manager) Option {
	return func(api *API) {
		api.webhooksStore = store
	}
}
