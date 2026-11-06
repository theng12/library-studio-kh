package stock

import "errors"

// Only provider identifiers are decoded; credentials in settings never leave this client.
func (b *Bazarr) ProviderNames() ([]string, error) {
	var raw struct {
		General struct {
			Providers *[]string `json:"enabled_providers"`
		} `json:"general"`
	}
	if err := b.do("GET", "/system/settings", nil, &raw); err != nil {
		return nil, err
	}
	if raw.General.Providers == nil {
		return nil, errors.New("Bazarr did not report configured subtitle providers")
	}
	return append([]string{}, (*raw.General.Providers)...), nil
}
