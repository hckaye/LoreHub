package httpapi

import discussionsapi "github.com/lorehub/lorehub/services/api/internal/discussions"

func WithDiscussions(store discussionsapi.Store) Option {
	return func(api *API) {
		api.discussionsStore = store
	}
}
