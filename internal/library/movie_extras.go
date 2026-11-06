package library

import (
	"fmt"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

// MovieExtras reads Radarr's artwork facts and collection lineup for one film.
func MovieExtras(c config.Config, app string, id int) (stock.MovieExtras, error) {
	if app != "Radarr" {
		return stock.MovieExtras{}, fmt.Errorf("movie extras are available for Radarr films")
	}
	a, err := arr(c, app)
	if err != nil {
		return stock.MovieExtras{}, err
	}
	x, err := a.MovieExtras(id)
	return x, a.SafeError(err)
}

// MovieArtwork reads one allowlisted poster or fanart through the Radarr client.
func MovieArtwork(c config.Config, app string, id int, name string) ([]byte, string, error) {
	if app != "Radarr" {
		return nil, "", fmt.Errorf("artwork is available for Radarr films")
	}
	a, err := arr(c, app)
	if err != nil {
		return nil, "", err
	}
	b, ct, err := a.Artwork(id, name)
	return b, ct, a.SafeError(err)
}
