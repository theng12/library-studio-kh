//go:build darwin

package platform

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func jobProperty(dict plistNode, key string) (plistNode, error) {
	if dict.XMLName.Local != "dict" || len(dict.Nodes)%2 != 0 {
		return plistNode{}, errors.New("invalid login-job dictionary")
	}
	var value plistNode
	found := false
	for i := 0; i < len(dict.Nodes); i += 2 {
		name := dict.Nodes[i]
		if name.XMLName.Local != "key" || len(name.Nodes) != 0 {
			return plistNode{}, errors.New("invalid login-job property")
		}
		if name.Content == key {
			if found {
				return plistNode{}, errors.New("ambiguous login-job property")
			}
			value = dict.Nodes[i+1]
			found = true
		}
	}
	if !found {
		return plistNode{}, errors.New("login job has no Library configuration ownership")
	}
	return value, nil
}

// A different Library home is a different host; a temporary/test home must never
// attach to, restart or replace the user's real background job.
func checkJobHome(path, home string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := xml.NewDecoder(io.LimitReader(file, (1<<20)+1))
	var doc plistNode
	if decoder.Decode(&doc) != nil || doc.XMLName.Local != "plist" || len(doc.Nodes) != 1 {
		return errors.New("login-job ownership could not be read")
	}
	var extra plistNode
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("login-job ownership is ambiguous")
	}
	env, err := jobProperty(doc.Nodes[0], "EnvironmentVariables")
	if err != nil {
		return err
	}
	value, err := jobProperty(env, "LIBRARY_STUDIO_HOME")
	if err != nil {
		return err
	}
	if value.XMLName.Local != "string" || len(value.Nodes) != 0 || !filepath.IsAbs(value.Content) || filepath.Clean(value.Content) != filepath.Clean(home) {
		return errors.New("the login job belongs to another Library configuration; it was left untouched")
	}
	return nil
}
