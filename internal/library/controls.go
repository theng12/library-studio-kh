package library

import (
	"encoding/json"
	"fmt"
	"strings"

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

// RemoveTorrents drops only qBittorrent's bookkeeping for each hash; the files
// always stay on disk (deleteFiles=false). It returns one message per failed
// hash and keeps going through the rest.
func RemoveTorrents(c config.Config, hashes []string) ([]string, error) {
	if len(hashes) == 0 || len(hashes) > 1000 {
		return nil, fmt.Errorf("choose between 1 and 1000 torrents")
	}
	qb, err := qbit(c)
	if err != nil {
		return nil, err
	}
	errs := []string{}
	for _, h := range hashes {
		err := qb.RemoveMetadata(h)
		switch {
		case err == nil:
		case isUnreachable(err): // the wrapped network error carries the address
			errs = append(errs, "qBittorrent could not be reached to remove "+h+": check the connection in Settings.")
		default:
			errs = append(errs, "qBittorrent could not remove "+h+": "+err.Error())
		}
	}
	return errs, nil
}

// Torrent returns qBittorrent's current entry for one hash, so a caller only
// acts on a download qBittorrent still lists.
func Torrent(c config.Config, hash string) (stock.Torrent, error) {
	qb, err := qbit(c)
	if err != nil {
		return stock.Torrent{}, err
	}
	ts, err := qb.Torrents()
	if err != nil {
		return stock.Torrent{}, fmt.Errorf("%s", sourceError("qBittorrent", err))
	}
	for _, t := range ts {
		if hash != "" && strings.EqualFold(t.Hash, hash) {
			return t, nil
		}
	}
	return stock.Torrent{}, fmt.Errorf("qBittorrent doesn't list that download")
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

// EnsureMediaRoot adds a folder to its owning app without changing existing roots.
func EnsureMediaRoot(c config.Config, app, path string) (stock.RootFolderSetup, error) {
	a, err := arr(c, app)
	if err != nil {
		return stock.RootFolderSetup{}, err
	}
	return a.EnsureRootFolder(stock.RootFolderInput{Path: path})
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
