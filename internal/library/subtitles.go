package library

import (
	"errors"
	"strings"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

type SubtitleView struct {
	Enabled           bool                    `json:"enabled"`
	Configured        bool                    `json:"configured"`
	Version           string                  `json:"version,omitempty"`
	PreferredLanguage string                  `json:"preferredLanguage"`
	Items             []stock.SubtitleMedia   `json:"items"`
	Profiles          []stock.SubtitleProfile `json:"profiles"`
	Providers         []string                `json:"providers"`
	Problems          []string                `json:"problems"`
}

func subtitleClient(c config.Config) (*stock.Bazarr, error) {
	if !c.BazarrOn {
		return nil, errors.New("Subtitle controls are off. Enable Bazarr in Settings.")
	}
	if c.Bazarr.URL == "" || c.Bazarr.APIKey == "" {
		return nil, errors.New("Connect Bazarr in Settings to use subtitles.")
	}
	return stock.NewBazarr(c.Bazarr.URL, c.Bazarr.APIKey), nil
}
func validSubtitleParent(app string, id int) bool {
	return (app == "Sonarr" || app == "Radarr") && id > 0
}

// Subtitles keeps an optional companion failure separate from the main library.
func Subtitles(c config.Config, app string, id int) (SubtitleView, error) {
	v := SubtitleView{Enabled: c.BazarrOn, Configured: c.Bazarr.URL != "" && c.Bazarr.APIKey != "", PreferredLanguage: "English", Items: []stock.SubtitleMedia{}, Profiles: []stock.SubtitleProfile{}, Providers: []string{}, Problems: []string{}}
	if !validSubtitleParent(app, id) {
		return v, errors.New("Choose a film or series for subtitles.")
	}
	b, err := subtitleClient(c)
	if err != nil {
		v.Problems = append(v.Problems, err.Error())
		return v, nil
	}
	if v.Version, err = b.Status(); err != nil {
		v.Problems = append(v.Problems, err.Error())
		return v, nil
	}
	if providers, e := b.ProviderNames(); e != nil {
		v.Problems = append(v.Problems, e.Error())
	} else {
		v.Providers = providers
		if len(providers) == 0 {
			v.Problems = append(v.Problems, "No subtitle providers are enabled in Bazarr.")
		}
	}
	if profiles, e := b.Profiles(); e != nil {
		v.Problems = append(v.Problems, e.Error())
	} else {
		v.Profiles = profiles
	}
	if items, e := b.Subtitles(strings.ToLower(app), id); e != nil {
		v.Problems = append(v.Problems, e.Error())
	} else {
		v.Items = items
	}
	return v, nil
}

func SearchEnglishSubtitles(c config.Config, app string, id, itemID int) error {
	if !validSubtitleParent(app, id) || itemID <= 0 {
		return errors.New("Choose a film or episode for subtitles.")
	}
	b, err := subtitleClient(c)
	if err != nil {
		return err
	}
	providers, err := b.ProviderNames()
	if err != nil {
		return err
	}
	if len(providers) == 0 {
		return errors.New("Enable a subtitle provider in Bazarr before searching.")
	}
	return b.EnglishSearch(strings.ToLower(app), id, itemID)
}
func AssignEnglishSubtitles(c config.Config, app string, id, profileID int) error {
	if !validSubtitleParent(app, id) || profileID <= 0 {
		return errors.New("Choose an English subtitle profile and a film or series.")
	}
	b, err := subtitleClient(c)
	if err != nil {
		return err
	}
	// Confirm the parent in the owning library before changing Bazarr's profile.
	a, err := arr(c, app)
	if err != nil {
		return err
	}
	if _, err = a.Detail(id); err != nil {
		return a.SafeError(err)
	}
	return b.AssignEnglishProfile(strings.ToLower(app), id, profileID)
}
