package stock

// metadata is the normalized subset of facts every library app reports on its
// parent resource. Missing facts stay nil or empty; nothing is guessed.
type metadata struct {
	Overview         string   `json:"overview"`
	Year             *int     `json:"year,omitempty"`
	Genres           []string `json:"genres"`
	RuntimeMinutes   *int     `json:"runtimeMinutes,omitempty"`
	Network          string   `json:"network,omitempty"`
	Certification    string   `json:"certification,omitempty"`
	OriginalLanguage string   `json:"originalLanguage,omitempty"`
}

// rawMetadata mirrors the apps' own field names. Lidarr has no year, runtime,
// network or certification on an artist, so those stay nil or empty.
type rawMetadata struct {
	Overview         string    `json:"overview"`
	Biography        string    `json:"biography"`
	Year             *int      `json:"year"`
	Genres           []string  `json:"genres"`
	Runtime          *int      `json:"runtime"`
	Network          string    `json:"network"`
	Certification    string    `json:"certification"`
	OriginalLanguage *language `json:"originalLanguage"`
}

// language is the originalLanguage object Sonarr and Radarr report.
type language struct {
	Name string `json:"name"`
}

func (r rawMetadata) metadata() metadata {
	m := metadata{
		Overview:       r.Overview,
		Year:           r.Year,
		Genres:         r.Genres,
		RuntimeMinutes: r.Runtime,
		Network:        r.Network,
		Certification:  r.Certification,
	}
	if m.Overview == "" {
		m.Overview = r.Biography // Lidarr's name for an artist's overview
	}
	if m.Genres == nil {
		m.Genres = []string{}
	}
	if r.OriginalLanguage != nil {
		m.OriginalLanguage = r.OriginalLanguage.Name
	}
	return m
}
