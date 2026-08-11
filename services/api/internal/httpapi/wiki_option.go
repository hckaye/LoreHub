package httpapi

import wikiapi "github.com/lorehub/lorehub/services/api/internal/wiki"

func WithWiki(store wikiapi.Store) Option {
	return func(api *API) {
		api.wikiStore = store
	}
}
