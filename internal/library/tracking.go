package library

import (
	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

// Detail reads one library item and its seasons, episodes or albums.
func Detail(c config.Config, app string, id int) (stock.Detail, error) {
	a, err := arr(c, app)
	if err != nil {
		return stock.Detail{}, err
	}
	d, err := a.Detail(id)
	return d, a.SafeError(err)
}

// SetTracking changes monitoring of one child of one library item.
func SetTracking(c config.Config, app string, id int, kind string, childID, seasonNumber int, monitored bool) error {
	a, err := arr(c, app)
	if err != nil {
		return err
	}
	return a.SafeError(a.SetTracking(id, kind, childID, seasonNumber, monitored))
}

// Releases deliberately searches one app for releases in the given context.
func Releases(c config.Config, app string, ctx stock.ReleaseContext) ([]stock.Release, error) {
	a, err := arr(c, app)
	if err != nil {
		return nil, err
	}
	items, err := a.Releases(ctx)
	return items, a.SafeError(err)
}

// Grab submits one previously searched release to the same app.
func Grab(c config.Config, app string, ctx stock.ReleaseContext, key string) error {
	a, err := arr(c, app)
	if err != nil {
		return err
	}
	return a.SafeError(a.Grab(ctx, key))
}
