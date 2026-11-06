package library

import (
	"encoding/json"
	"fmt"

	"librarystudio/internal/config"
	"librarystudio/internal/stock"
)

func qbit(c config.Config) (*stock.QBit, error) {
	if c.QBit.URL == "" {
		return nil, fmt.Errorf("qBittorrent isn't connected. Add it in Settings first.")
	}
	return stock.NewQBit(c.QBit.URL, c.QBit.Username, c.QBit.Password), nil
}

// Downloads lists qBittorrent's queue.
func Downloads(c config.Config) ([]stock.Torrent, error) {
	qb, err := qbit(c)
	if err != nil {
		return nil, err
	}
	return qb.Torrents()
}

// TorrentAction pauses or resumes torrents by hash. It never deletes.
func TorrentAction(c config.Config, hashes []string, action string) error {
	qb, err := qbit(c)
	if err != nil {
		return err
	}
	return qb.TorrentAction(hashes, action)
}

func arr(c config.Config, app string) (*stock.Arr, error) {
	for _, a := range arrs(c) {
		if a.Name == app {
			return a, nil
		}
	}
	return nil, fmt.Errorf("%s isn't connected", app)
}

// MediaOptions reads the live quality profile, folder and metadata choices for one app.
func MediaOptions(c config.Config, app string) (stock.Options, error) {
	a, err := arr(c, app)
	if err != nil {
		return stock.Options{}, err
	}
	return a.Options()
}

// AddSelected adds a Find result only with choices the app currently offers.
func AddSelected(c config.Config, app string, raw json.RawMessage, sel stock.AddSelection) (string, error) {
	a, err := arr(c, app)
	if err != nil {
		return "", err
	}
	return a.AddSelected(raw, sel)
}

// Monitor turns monitoring on or off for one library item.
func Monitor(c config.Config, app string, id int, monitored bool) error {
	a, err := arr(c, app)
	if err != nil {
		return err
	}
	return a.SetMonitored(id, monitored)
}
